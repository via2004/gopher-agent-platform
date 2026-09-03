package mcp

import "errors"

var (
	ErrInvalidWeatherClient  = errors.New("weather client is invalid")
	ErrInvalidEndpoint       = errors.New("MCP endpoint is invalid")
	ErrInvalidTimeout        = errors.New("MCP call timeout is invalid")
	ErrConnectFailed         = errors.New("connect MCP server failed")
	ErrListToolsFailed       = errors.New("list MCP tools failed")
	ErrWeatherToolNotFound   = errors.New("get_weather MCP tool is not available")
	ErrToolDefinitionInvalid = errors.New("MCP tool definition is invalid")
	ErrToolNotAllowed        = errors.New("MCP tool is not allowed")
	ErrArgumentsInvalid      = errors.New("MCP tool arguments are invalid")
	ErrCallToolFailed        = errors.New("call MCP tool failed")
	ErrToolFailed            = errors.New("MCP tool returned an error")
	ErrToolResultInvalid     = errors.New("MCP tool result is invalid")
)
