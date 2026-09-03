package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"gopherai/internal/weather"
)

const WeatherToolName = "get_weather"

// WeatherClient 是 MCP Server 依赖的天气查询边界，便于替换为测试实现。
type WeatherClient interface {
	Get(context.Context, string) (*weather.Result, error)
}

// WeatherInput 是 get_weather 工具的参数，SDK 会据此生成 JSON Schema。
type WeatherInput struct {
	City string `json:"city" jsonschema:"city to query"`
}

// NewWeatherServer 创建只暴露 get_weather 工具的 MCP Server。
func NewWeatherServer(client WeatherClient) (*mcpsdk.Server, error) {
	if client == nil {
		return nil, ErrInvalidWeatherClient
	}

	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "gopherai-weather-server",
		Version: "0.1.0",
	}, nil)
	readOnly, destructive := true, false
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        WeatherToolName,
		Description: "query current weather for a city",
		Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &destructive},
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input WeatherInput) (*mcpsdk.CallToolResult, *weather.Result, error) {
		result, err := client.Get(ctx, input.City)
		if err != nil {
			return nil, nil, err
		}
		return nil, result, nil
	})

	return server, nil
}
