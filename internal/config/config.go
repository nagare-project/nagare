// Package config 负责配置文件的定位、首次生成与读写。
//
// 路径遵循各平台惯例（macOS: ~/Library/Application Support；Linux: XDG；
// Windows: %AppData%），可用环境变量 NAGARE_CONFIG_DIR 覆盖（测试与便携模式用）。
// 配置里存着鉴权 token，文件权限必须保持 0600 —— 这是决议 A4 的一部分。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/nagare-player/nagare/internal/random"
)

// DefaultPort 是首次运行时的偏好端口，被占用时会自动向上递增并回写配置。
const DefaultPort = 8590

// EnvConfigDir 是覆盖配置目录的环境变量名。
const EnvConfigDir = "NAGARE_CONFIG_DIR"

// tokenBytes 是鉴权 token 的随机字节数（128 位）。
const tokenBytes = 16

// maxPort 是合法 TCP 端口上限。
const maxPort = 65535

// Config 是落盘的全部配置。
type Config struct {
	// Port 是偏好端口；实际监听端口可能更高（被占时向上找），启动时会回写。
	Port int `toml:"port"`
	// Token 是 128 位鉴权 token 的十六进制表示，随配置文件以 0600 权限保存。
	Token string `toml:"token"`
}

// Dir 返回配置目录路径（不保证已创建）。
func Dir() (string, error) {
	if dir := os.Getenv(EnvConfigDir); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("定位系统配置目录: %w", err)
	}
	return filepath.Join(base, "nagare"), nil
}

// Path 返回配置文件完整路径。
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// NewToken 生成一个新的 128 位鉴权 token。
func NewToken() string {
	return random.Hex(tokenBytes)
}

// LoadOrInit 读取配置；文件不存在时生成新配置（含随机 token）并落盘。
// 已存在但缺 token / 端口非法的配置会被自愈后回写，避免带着空 token 启动。
func LoadOrInit() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := &Config{Port: DefaultPort, Token: NewToken()}
		if err := cfg.Save(); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取配置 %s: %w", path, err)
	}

	var cfg Config
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置 %s: %w", path, err)
	}

	healed := false
	if cfg.Token == "" {
		cfg.Token = NewToken()
		healed = true
	}
	if cfg.Port <= 0 || cfg.Port > maxPort {
		cfg.Port = DefaultPort
		healed = true
	}
	if healed {
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

// Save 把配置写盘：目录 0700，文件 0600（含 token，权限是安全要求而非习惯）。
func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建配置目录: %w", err)
	}
	// MkdirAll 只对新建的目录生效 0700，已存在的宽权限目录要显式收紧。
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("收紧配置目录权限 %s: %w", dir, err)
	}
	buf, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置: %w", err)
	}
	// 先写临时文件再原子改名：中途断电/磁盘满不会把现有配置截成半截
	// —— 解析失败是硬错误（不静默重建），坏文件会让下次启动直接失败。
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("写入临时配置 %s: %w", tmp, err)
	}
	// WriteFile 对已存在的文件不会改权限，这里显式收紧一次。
	if err := os.Chmod(tmp, 0o600); err != nil {
		return fmt.Errorf("收紧配置权限 %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("替换配置 %s: %w", path, err)
	}
	return nil
}
