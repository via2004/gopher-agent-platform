package weather

// Result 是 MCP get_weather 工具对外暴露的稳定天气结果。
type Result struct {
	Location        string  `json:"location"`
	TemperatureC    float64 `json:"temperature_c"`
	Condition       string  `json:"condition"`
	HumidityPercent int     `json:"humidity_percent"`
	WindSpeedKMPH   float64 `json:"wind_speed_kmph"`
}
