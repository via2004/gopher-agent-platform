package baidutts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"gopherai/internal/tts"
)

const (
	defaultBaseURL        = "https://aip.baidubce.com"
	defaultRequestTimeout = 10 * time.Second
	maxResponseBytes      = 1 << 20
	tokenRefreshSkew      = time.Minute
	maxTokenLifetime      = 365 * 24 * time.Hour

	tokenPath  = "/oauth/2.0/token"
	createPath = "/rpc/2.0/tts/v1/create"
	queryPath  = "/rpc/2.0/tts/v1/query"
)

type Config struct {
	APIKey     string
	SecretKey  string
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Client 将百度 OAuth 和长文本语音合成 API 适配为 tts.Provider。
type Client struct {
	apiKey     string
	secretKey  string
	baseURL    *url.URL
	timeout    time.Duration
	httpClient *http.Client

	tokenMu        sync.Mutex
	token          string
	tokenExpiresAt time.Time
}

// NewClient 校验 Provider 配置并创建可复用的 TTS Client。
func NewClient(config Config) (*Client, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, ErrInvalidAPIKey
	}
	secretKey := strings.TrimSpace(config.SecretKey)
	if secretKey == "" {
		return nil, ErrInvalidSecretKey
	}

	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrInvalidBaseURL
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrInvalidBaseURL
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	return &Client{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    parsed,
		timeout:    timeout,
		httpClient: httpClient,
	}, nil
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int64  `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if err := ctx.Err(); err != nil {
		return "", err
	}
	// 当前时间+1分钟 < Token 过期时间,判定这个token还可用,直接复用
	if c.token != "" && time.Now().Add(tokenRefreshSkew).Before(c.tokenExpiresAt) {
		return c.token, nil
	}

	endpoint := c.endpoint(tokenPath)
	query := endpoint.Query()
	query.Set("grant_type", "client_credentials")
	query.Set("client_id", c.apiKey)
	query.Set("client_secret", c.secretKey)
	endpoint.RawQuery = query.Encode()

	body, err := c.do(ctx, http.MethodPost, endpoint, nil, ErrTokenFailed)
	if err != nil {
		return "", err
	}
	var response tokenResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("%w: decode token response", ErrResponseInvalid)
	}
	response.AccessToken = strings.TrimSpace(response.AccessToken)
	if response.Error != "" {
		return "", ErrTokenFailed
	}
	if response.AccessToken == "" || response.ExpiresIn <= 0 || response.ExpiresIn > int64(maxTokenLifetime/time.Second) {
		return "", ErrResponseInvalid
	}

	c.token = response.AccessToken
	c.tokenExpiresAt = time.Now().Add(time.Duration(response.ExpiresIn) * time.Second)
	return c.token, nil
}

type createRequest struct {
	Text           []string `json:"text"`
	Format         string   `json:"format"`
	Voice          int      `json:"voice"`
	Language       string   `json:"lang"`
	Speed          int      `json:"speed"`
	Pitch          int      `json:"pitch"`
	Volume         int      `json:"volume"`
	EnableSubtitle int      `json:"enable_subtitle"`
}

type createResponse struct {
	TaskID     string `json:"task_id"`
	TaskStatus string `json:"task_status"`
	ErrorCode  *int64 `json:"error_code"`
	ErrorMsg   string `json:"error_msg"`
}

// Create 提交一个长文本语音合成任务，并返回 Provider 生成的任务 ID。
func (c *Client) Create(ctx context.Context, text string) (*tts.Task, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, tts.ErrInvalidText
	}

	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(createRequest{
		Text:           []string{text},
		Format:         "mp3-16k",
		Voice:          4194,
		Language:       "zh",
		Speed:          5,
		Pitch:          5,
		Volume:         5,
		EnableSubtitle: 0,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal create request: %w", err)
	}
	endpoint := c.providerEndpoint(createPath, token)
	body, err := c.do(ctx, http.MethodPost, endpoint, payload, ErrProviderFailed)
	if err != nil {
		return nil, err
	}

	var response createResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("%w: decode create response", ErrResponseInvalid)
	}
	response.TaskID = strings.TrimSpace(response.TaskID)
	if providerRejected(response.ErrorCode) {
		return nil, ErrProviderFailed
	}
	if response.TaskID == "" || (response.TaskStatus != "" && response.TaskStatus != "Running") {
		return nil, ErrResponseInvalid
	}
	return &tts.Task{ID: response.TaskID, Status: tts.StatusRunning}, nil
}

type queryRequest struct {
	TaskIDs []string `json:"task_ids"`
}

type queryResponse struct {
	TasksInfo []providerTask `json:"tasks_info"`
	ErrorCode *int64         `json:"error_code"`
	ErrorMsg  string         `json:"error_msg"`
}

type providerTask struct {
	TaskID     string              `json:"task_id"`
	TaskStatus string              `json:"task_status"`
	TaskResult *providerTaskResult `json:"task_result"`
}

type providerTaskResult struct {
	SpeechURL string `json:"speech_url"`
	ErrorNo   int64  `json:"err_no"`
	ErrorMsg  string `json:"err_msg"`
}

// Get 查询一个 Provider 任务，并将其状态转换为稳定的 TTS 领域模型。
func (c *Client) Get(ctx context.Context, taskID string) (*tts.Task, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, tts.ErrInvalidTaskID
	}

	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(queryRequest{TaskIDs: []string{taskID}})
	if err != nil {
		return nil, fmt.Errorf("marshal query request: %w", err)
	}
	endpoint := c.providerEndpoint(queryPath, token)
	body, err := c.do(ctx, http.MethodPost, endpoint, payload, ErrProviderFailed)
	if err != nil {
		return nil, err
	}

	var response queryResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("%w: decode query response", ErrResponseInvalid)
	}
	if providerRejected(response.ErrorCode) {
		return nil, ErrProviderFailed
	}
	if len(response.TasksInfo) != 1 {
		return nil, ErrResponseInvalid
	}
	return taskFromProvider(taskID, response.TasksInfo[0])
}

func taskFromProvider(requestedID string, provider providerTask) (*tts.Task, error) {
	provider.TaskID = strings.TrimSpace(provider.TaskID)
	if provider.TaskID == "" || provider.TaskID != requestedID {
		return nil, ErrResponseInvalid
	}

	switch provider.TaskStatus {
	case "Running":
		return &tts.Task{ID: provider.TaskID, Status: tts.StatusRunning}, nil
	case "Success":
		if provider.TaskResult == nil {
			return nil, ErrResponseInvalid
		}
		audioURL := strings.TrimSpace(provider.TaskResult.SpeechURL)
		if !validAudioURL(audioURL) {
			return nil, ErrResponseInvalid
		}
		return &tts.Task{ID: provider.TaskID, Status: tts.StatusSucceeded, AudioURL: &audioURL}, nil
	case "Failure":
		errorCode := "provider_task_failed"
		return &tts.Task{ID: provider.TaskID, Status: tts.StatusFailed, ErrorCode: &errorCode}, nil
	default:
		return nil, ErrResponseInvalid
	}
}

func validAudioURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func providerRejected(code *int64) bool {
	return code != nil && *code != 0
}

func (c *Client) providerEndpoint(endpointPath, token string) *url.URL {
	endpoint := c.endpoint(endpointPath)
	query := endpoint.Query()
	query.Set("access_token", token)
	endpoint.RawQuery = query.Encode()
	return endpoint
}

func (c *Client) endpoint(endpointPath string) *url.URL {
	endpoint := *c.baseURL
	endpoint.Path = path.Join(c.baseURL.Path, endpointPath)
	endpoint.RawPath = ""
	return &endpoint
}

func (c *Client) do(ctx context.Context, method string, endpoint *url.URL, payload []byte, sentinel error) ([]byte, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(requestCtx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("%w: create request", sentinel)
	}
	// 期望接收一个JSON
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		if contextErr := requestCtx.Err(); contextErr != nil {
			return nil, contextErr
		}
		if errors.Is(err, context.Canceled) {
			return nil, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, sentinel
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: provider returned HTTP %d", sentinel, response.StatusCode)
	}

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if contextErr := requestCtx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, fmt.Errorf("%w: read response", sentinel)
	}
	if len(responseBody) > maxResponseBytes {
		return nil, ErrResponseTooLarge
	}
	return responseBody, nil
}
