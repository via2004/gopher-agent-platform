package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL        = "https://wttr.in"
	defaultRequestTimeout = 5 * time.Second
	maxCityLength         = 100
	maxResponseBytes      = 1 << 20
)

// Config 配置天气 Provider 的地址、超时和 HTTP 客户端。
type Config struct {
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Client 调用兼容 wttr.in JSON 格式的天气 Provider。
type Client struct {
	baseURL    *url.URL
	timeout    time.Duration
	httpClient *http.Client
}

// NewClient 创建天气 Provider 客户端。
func NewClient(config Config) (*Client, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
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
		baseURL:    parsed,
		timeout:    timeout,
		httpClient: httpClient,
	}, nil
}

// providerResponse 只描述 wttr.in 的响应格式，避免让 Provider 字段泄漏到业务层。
type providerResponse struct {
	CurrentCondition []struct {
		TempC         string `json:"temp_C"`
		Humidity      string `json:"humidity"`
		WindSpeedKMPH string `json:"windspeedKmph"`
		WeatherDesc   []struct {
			Value string `json:"value"`
		} `json:"weatherDesc"`
	} `json:"current_condition"`
	NearestArea []struct {
		AreaName []struct {
			Value string `json:"value"`
		} `json:"areaName"`
	} `json:"nearest_area"`
}

// Get 查询指定城市当前天气，并将 Provider 响应转换为项目自己的结果模型。
func (c *Client) Get(ctx context.Context, city string) (*Result, error) {
	// 先在本地拒绝无效输入，避免无意义地访问外部 Provider。
	city = strings.TrimSpace(city)
	if city == "" || len([]rune(city)) > maxCityLength {
		return nil, ErrInvalidCity
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	/*  https://wttr.in/api/
	endpoint.Scheme = "https"
	endpoint.Host   = "wttr.in"
	endpoint.Path   = "/api/"
	*/
	endpoint := *c.baseURL
	basePath := strings.TrimRight(endpoint.Path, "/")

	// 适合放进URL的编码路径：/api/%E4%B8%8A%E6%B5%B7
	baseEscapedPath := strings.TrimRight(endpoint.EscapedPath(), "/")
	endpoint.Path = basePath + "/" + city
	// Path 保存未转义值，RawPath 保存对应的单次转义值，避免 URL.String 再次转义 %。
	endpoint.RawPath = baseEscapedPath + "/" + url.PathEscape(city)
	query := endpoint.Query()
	query.Set("format", "j1")          // 要求返回JSON
	query.Set("lang", "zh")            // 要求中文天气描述
	endpoint.RawQuery = query.Encode() // 将参数编码回URL

	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// 子 Context 同时受调用方取消和本次请求超时控制。
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %w", ErrProviderFailed, err)
	}
	request.Header.Set("Accept", "application/json") // 声明希望收到JSON

	response, err := c.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrProviderFailed, err)
	}
	defer response.Body.Close()

	// 只接受2xx之类的成功响应，所有失败响应统一映射成ErrProvierFailed
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: provider returned HTTP %d", ErrProviderFailed, response.StatusCode)
	}

	// 限制响应体大小，防止异常 Provider 返回过大的内容占用内存。
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %w", ErrProviderFailed, err)
	}
	if len(body) > maxResponseBytes {
		return nil, ErrResponseTooLarge
	}

	// Provider 使用字符串表示数值，这里解析并转换为项目自己的数值字段。
	var payload providerResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: decode JSON: %w", ErrResponseInvalid, err)
	}
	if len(payload.CurrentCondition) == 0 {
		return nil, fmt.Errorf("%w: current condition is missing", ErrResponseInvalid)
	}

	current := payload.CurrentCondition[0]
	temperature, err := strconv.ParseFloat(strings.TrimSpace(current.TempC), 64)
	if err != nil {
		return nil, fmt.Errorf("%w: temperature: %w", ErrResponseInvalid, err)
	}
	humidity, err := strconv.Atoi(strings.TrimSpace(current.Humidity))
	if err != nil {
		return nil, fmt.Errorf("%w: humidity: %w", ErrResponseInvalid, err)
	}
	windSpeed, err := strconv.ParseFloat(strings.TrimSpace(current.WindSpeedKMPH), 64)
	if err != nil {
		return nil, fmt.Errorf("%w: wind speed: %w", ErrResponseInvalid, err)
	}
	if humidity < 0 || humidity > 100 || len(current.WeatherDesc) == 0 || strings.TrimSpace(current.WeatherDesc[0].Value) == "" {
		return nil, fmt.Errorf("%w: weather fields are invalid", ErrResponseInvalid)
	}

	location := city
	if len(payload.NearestArea) > 0 && len(payload.NearestArea[0].AreaName) > 0 {
		if value := strings.TrimSpace(payload.NearestArea[0].AreaName[0].Value); value != "" {
			location = value
		}
	}

	return &Result{
		Location:        location,
		TemperatureC:    temperature,
		Condition:       strings.TrimSpace(current.WeatherDesc[0].Value),
		HumidityPercent: humidity,
		WindSpeedKMPH:   windSpeed,
	}, nil
}
