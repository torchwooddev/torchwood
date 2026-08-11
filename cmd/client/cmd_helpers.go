package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

// jsonStringList 解析 JSON 字符串数组 flag（如 --permissions '["a","b"]'、
// --queries '["equal(\"status\",\"active\")"]'），空串返回 nil。
func jsonStringList(s, flagName string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	var v []string
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("--%s 解析失败（需 JSON 字符串数组）：%v", flagName, err)
	}
	return v, nil
}

// jsonStringMap 解析 JSON 对象 flag（map<string,string>，如 --metadata、--vars）。
func jsonStringMap(s, flagName string) (map[string]string, error) {
	if s == "" {
		return nil, nil
	}
	var v map[string]string
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("--%s 解析失败（需 JSON 对象）：%v", flagName, err)
	}
	return v, nil
}

// jsonInt64Map 解析 JSON 对象 flag（map<string,int64>，如 --increment '{"views":1}'）。
func jsonInt64Map(s, flagName string) (map[string]int64, error) {
	if s == "" {
		return nil, nil
	}
	var v map[string]int64
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("--%s 解析失败（需 JSON 对象，值为整数）：%v", flagName, err)
	}
	return v, nil
}

// structData 把 JSON 对象 flag（document data / team prefs 本体）解析为
// Struct；空串返回 nil，非对象 JSON 报错。
func structData(s string) (*structpb.Struct, error) {
	if s == "" {
		return nil, nil
	}
	st := &structpb.Struct{}
	if err := protojson.Unmarshal([]byte(s), st); err != nil {
		return nil, fmt.Errorf("--data 解析失败（需 JSON 对象）：%v", err)
	}
	return st, nil
}

// changedInt32Ptr 返回 flag 显式设置时的指针（nil 表示未设置），
// 用于映射 proto3 optional int32 字段的 presence 语义。
func changedInt32Ptr(cmd *cobra.Command, name string, v int32) *int32 {
	if cmd.Flags().Changed(name) {
		return &v
	}
	return nil
}

// changedStringPtr 返回 flag 显式设置时的指针（nil 表示未设置），
// 用于映射 proto3 optional string 字段的 presence 语义。
func changedStringPtr(cmd *cobra.Command, name string, v string) *string {
	if cmd.Flags().Changed(name) {
		return &v
	}
	return nil
}
