package i18n

import (
	"encoding/json"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type LocalizerWrapper struct {
	*i18n.Localizer
	Locale string
}

func Init(i18nPath string) (*i18n.Bundle, error) {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	embeddedFiles := GetEmbeddedFiles()
	if len(embeddedFiles) > 0 {
		for filename, data := range embeddedFiles {
			if len(data) > 0 {

				messageFile, err := bundle.ParseMessageFileBytes(data, filename)
				if err != nil {
					continue
				}
				bundle.AddMessages(messageFile.Tag, messageFile.Messages...)
			}
		}
		if len(bundle.LanguageTags()) > 0 {
			return bundle, nil
		}
	}
	possiblePaths := []string{
		i18nPath,
		filepath.Join(".", i18nPath),
		filepath.Join(getExecutableDir(), i18nPath),
		filepath.Join(getExecutableDir(), "..", i18nPath),
	}

	var lastErr error
	for _, path := range possiblePaths {
		files, err := os.ReadDir(path)
		if err != nil {
			lastErr = err
			continue
		}

		// 找到有效路径，加载文件
		for _, file := range files {
			if !file.IsDir() && (filepath.Ext(file.Name()) == ".json") {
				filePath := filepath.Join(path, file.Name())
				bundle.MustLoadMessageFile(filePath)
			}
		}
		return bundle, nil
	}

	return nil, lastErr
}

// getExecutableDir 获取可执行文件所在目录
func getExecutableDir() string {
	ex, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(ex)
}

func NewLocalizer(bundle *i18n.Bundle, lang string) *LocalizerWrapper {
	return &LocalizerWrapper{
		Localizer: i18n.NewLocalizer(bundle, lang),
		Locale:    lang,
	}
}

func DetectSystemLanguage() string {

	envVars := []string{"LANG", "LANGUAGE", "LC_ALL", "LC_MESSAGES"}

	for _, envVar := range envVars {
		if lang := os.Getenv(envVar); lang != "" {
			if isChinese(lang) {
				return "zh"
			}

			return "en"
		}
	}

	if runtime.GOOS == "windows" {
		return detectWindowsLanguage()
	}

	return "en"
}

func isChinese(lang string) bool {
	lang = strings.ToLower(lang)
	chineseIndicators := []string{
		"zh", "chinese", "china", "cn", "zh_cn", "zh_tw", "zh_hk", "zh_sg",
		"zh-cn", "zh-tw", "zh-hk", "zh-sg", "chs", "cht",
	}

	for _, indicator := range chineseIndicators {
		if strings.Contains(lang, indicator) {
			return true
		}
	}
	return false
}

func detectWindowsLanguage() string {

	windowsEnvVars := []string{"LANG", "LANGUAGE"}

	for _, envVar := range windowsEnvVars {
		if lang := os.Getenv(envVar); lang != "" {
			if isChinese(lang) {
				return "zh"
			}
		}
	}

	if lang := getWindowsSystemLanguageViaPowerShell(); lang != "" {
		if isChinese(lang) {
			return "zh"
		}
		return "en"
	}

	// 尝试通过wmic获取系统语言
	if lang := getWindowsSystemLanguageViaWMIC(); lang != "" {
		if isChinese(lang) {
			return "zh"
		}
		return "en"
	}

	// 默认返回英语
	return "en"
}

// getWindowsSystemLanguageViaPowerShell 通过PowerShell获取系统语言
func getWindowsSystemLanguageViaPowerShell() string {
	cmd := exec.Command("powershell", "-Command", "Get-Culture | Select-Object -ExpandProperty Name")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// getWindowsSystemLanguageViaWMIC 通过WMIC获取系统语言
func getWindowsSystemLanguageViaWMIC() string {
	cmd := exec.Command("wmic", "os", "get", "locale", "/value")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	// 解析WMIC输出
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Locale=") {
			locale := strings.TrimPrefix(line, "Locale=")
			locale = strings.TrimSpace(locale)

			// 中文系统的Locale代码
			chineseLocales := []string{"0804", "0404", "0C04", "1004", "1404"}
			for _, chLoc := range chineseLocales {
				if strings.Contains(locale, chLoc) {
					return "zh"
				}
			}
		}
	}
	return ""
}
