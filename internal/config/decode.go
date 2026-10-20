package config

import (
	"encoding"
	"fmt"
	"reflect"
	"strings"
)

// Decode 把语法树解码到 out（必须是非空指针）。
// 未识别的键会聚合到同一个错误里。
func Decode(n *Node, out any) error {
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("Decode 需要非空指针")
	}
	var unknown []string
	if err := decodeInto(n, rv.Elem(), "", &unknown); err != nil {
		return err
	}
	if len(unknown) > 0 {
		return fmt.Errorf("未知配置项: %s", strings.Join(unknown, ", "))
	}
	return nil
}

func decodeInto(n *Node, rv reflect.Value, path string, unknown *[]string) error {
	if !rv.CanSet() {
		return fmt.Errorf("%s: 字段不可写", path)
	}
	if n.Kind == KindString && rv.CanAddr() {
		if u, ok := rv.Addr().Interface().(encoding.TextUnmarshaler); ok {
			if err := u.UnmarshalText([]byte(n.Str)); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			return nil
		}
	}
	switch rv.Kind() {
	case reflect.Ptr:
		if n.Kind != KindTable {
			return typeErr(path, "表")
		}
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		return decodeInto(n, rv.Elem(), path, unknown)
	case reflect.Struct:
		if n.Kind != KindTable {
			return typeErr(path, "表")
		}
		fields := map[string]reflect.Value{}
		for i := 0; i < rv.NumField(); i++ {
			f := rv.Type().Field(i)
			if f.PkgPath != "" {
				continue
			}
			tag := f.Tag.Get("toml")
			if tag == "" || tag == "-" {
				continue
			}
			fields[tag] = rv.Field(i)
		}
		for key, child := range n.Table {
			fv, ok := fields[key]
			if !ok {
				*unknown = append(*unknown, joinPath(path, key))
				continue
			}
			if err := decodeInto(child, fv, joinPath(path, key), unknown); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if n.Kind != KindTable {
			return typeErr(path, "表")
		}
		if rv.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("%s: 仅支持字符串键", path)
		}
		if rv.IsNil() {
			rv.Set(reflect.MakeMap(rv.Type()))
		}
		for key, child := range n.Table {
			elem := reflect.New(rv.Type().Elem()).Elem()
			if err := decodeInto(child, elem, joinPath(path, key), unknown); err != nil {
				return err
			}
			rv.SetMapIndex(reflect.ValueOf(key), elem)
		}
		return nil
	case reflect.Slice:
		if n.Kind != KindArray {
			return typeErr(path, "数组")
		}
		if rv.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("%s: 仅支持字符串数组", path)
		}
		out := reflect.MakeSlice(rv.Type(), 0, len(n.Arr))
		for i, item := range n.Arr {
			if item.Kind != KindString {
				return typeErr(fmt.Sprintf("%s[%d]", path, i), "字符串")
			}
			out = reflect.Append(out, reflect.ValueOf(item.Str))
		}
		rv.Set(out)
		return nil
	case reflect.String:
		if n.Kind != KindString {
			return typeErr(path, "字符串")
		}
		rv.SetString(n.Str)
		return nil
	case reflect.Bool:
		if n.Kind != KindBool {
			return typeErr(path, "布尔")
		}
		rv.SetBool(n.Bool)
		return nil
	case reflect.Int, reflect.Int64:
		if n.Kind != KindInt {
			return typeErr(path, "整数")
		}
		rv.SetInt(n.Int)
		return nil
	default:
		return fmt.Errorf("%s: 不支持的类型 %s", path, rv.Type())
	}
}

func typeErr(path, want string) error {
	if path == "" {
		return fmt.Errorf("期望%s", want)
	}
	return fmt.Errorf("%s: 期望%s", path, want)
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}
