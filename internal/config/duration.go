package config

import "time"

// Duration 是 TOML 中以字符串书写的时长，如 "4s"、"1500ms"。
type Duration time.Duration

// UnmarshalText 实现 encoding.TextUnmarshaler。
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// Std 返回标准库时长。
func (d Duration) Std() time.Duration { return time.Duration(d) }

// String 返回 "4s" 这类文本形式。
func (d Duration) String() string { return time.Duration(d).String() }
