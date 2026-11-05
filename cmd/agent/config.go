package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是应用配置结构，对应 configs/config.yaml。
type Config struct {
	LLM struct {
		Provider    string  `yaml:"provider"`
		Model       string  `yaml:"model"`
		BaseURL     string  `yaml:"base_url"`
		APIKeyEnv   string  `yaml:"api_key_env"`
		Temperature float64 `yaml:"temperature"`
		MaxTokens   int     `yaml:"max_tokens"`
	} `yaml:"llm"`
	Agent struct {
		MaxIterations int `yaml:"max_iterations"`
	} `yaml:"agent"`
	Context struct {
		// MaxPromptTokens < 0 关闭上下文组装；0 或缺省使用默认值 6000。
		MaxPromptTokens     int `yaml:"max_prompt_tokens"`
		RecentMessages      int `yaml:"recent_messages"`
		MaxToolOutputTokens int `yaml:"max_tool_output_tokens"`
		SummaryMaxTokens    int `yaml:"summary_max_tokens"`
	} `yaml:"context"`
	Tools struct {
		Enabled []string `yaml:"enabled"`
	} `yaml:"tools"`
	Memory struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"memory"`
	Logging struct {
		Level string `yaml:"level"`
	} `yaml:"logging"`
}

// loadConfig 从 YAML 文件加载应用配置。
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
