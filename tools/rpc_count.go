package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func countRPC(dir string) (int, error) {
	re := regexp.MustCompile(`^\s*rpc\s+\w+`)
	count := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".proto") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				count++
			}
		}
		return nil
	})
	return count, err
}

func main() {
	client, _ := countRPC("proto/client")
	server, _ := countRPC("proto/server")
	console, _ := countRPC("proto/console")
	total := client + server + console
	fmt.Printf("client:%d server:%d console:%d total:%d\n", client, server, console, total)
	// swagger operationId count (optional, requires genproto)
	swaggerCount := 0
	_ = filepath.Walk("genproto", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".swagger.json") {
			return nil
		}
		data, _ := os.ReadFile(path)
		swaggerCount += strings.Count(string(data), "\"operationId\"")
		return nil
	})
	fmt.Printf("swagger operationId count: %d\n", swaggerCount)
	// 验证与文档一致性（可扩展为写入文档）
	if total != 187 {
		fmt.Printf("warning: total %d != expected 187, docs may need update\n", total)
	}
}
