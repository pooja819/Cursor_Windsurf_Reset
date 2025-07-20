package i18n

import (
	_ "embed"
)

//go:embed zh.json
var zhJSON []byte

//go:embed en.json
var enJSON []byte

// GetEmbeddedFiles 返回嵌入的语言文件
func GetEmbeddedFiles() map[string][]byte {
	return map[string][]byte{
		"zh.json": zhJSON,
		"en.json": enJSON,
	}
}
