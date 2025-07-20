package cleaner

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"Cursor_Windsurf_Reset/config"
	appi18n "Cursor_Windsurf_Reset/i18n"
	"github.com/google/uuid"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/rs/zerolog"
	_ "modernc.org/sqlite"
)

var log zerolog.Logger

func init() {
	log = zerolog.New(os.Stderr).With().Timestamp().Logger()
}

type Engine struct {
	config        *config.Config
	backupBaseDir string
	appDataPaths  map[string]string
	dryRun        bool
	verbose       bool
	progressChan  chan ProgressUpdate
	localizer     *appi18n.LocalizerWrapper
}

type ProgressUpdate struct {
	Type     string  `json:"type"`
	Message  string  `json:"message"`
	Progress float64 `json:"progress"`
	AppName  string  `json:"app_name,omitempty"`
	Phase    string  `json:"phase,omitempty"`
}

type CacheStats struct {
	DirCount    int
	TotalSize   int64
	TotalFiles  int
	CleanedDirs int
}

func NewEngine(cfg *config.Config, dryRun, verbose bool, localizer *appi18n.LocalizerWrapper) *Engine {
	engine := &Engine{
		config:       cfg,
		dryRun:       dryRun,
		verbose:      verbose,
		progressChan: make(chan ProgressUpdate, 100),
		localizer:    localizer,
	}

	engine.setupBackupDirectory()

	engine.discoverAppDataPaths()

	return engine
}

func (e *Engine) GetProgressChannel() <-chan ProgressUpdate {
	return e.progressChan
}

func (e *Engine) setupBackupDirectory() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get home directory")
		homeDir = "."
	}

	e.backupBaseDir = filepath.Join(homeDir, "CursorWindsurf_Advanced_Backups")
	if err := os.MkdirAll(e.backupBaseDir, 0755); err != nil {
		log.Error().Err(err).Msg("Failed to create backup directory")
	}
}

// discoverAppDataPaths discovers application data paths
func (e *Engine) discoverAppDataPaths() {
	e.appDataPaths = make(map[string]string)
	osType := runtime.GOOS

	for appName, appConfig := range e.config.Applications {
		e.appDataPaths[appName] = ""

		paths, exists := appConfig.DataPaths[osType]
		if !exists {
			continue
		}

		for _, pathTemplate := range paths {
			expandedPath := e.expandPathTemplate(pathTemplate)

			if _, err := os.Stat(expandedPath); err == nil {
				e.appDataPaths[appName] = expandedPath
				break
			}
		}
	}
}

func (e *Engine) expandPathTemplate(template string) string {
	if strings.HasPrefix(template, "~") {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			template = strings.Replace(template, "~", homeDir, 1)
		} else {
			log.Warn().Err(err).Msg("Failed to get home directory")
		}
	}

	result := os.Expand(template, func(key string) string {
		return os.Getenv(key)
	})

	re := regexp.MustCompile(`%([^%]+)%`)
	result = re.ReplaceAllStringFunc(result, func(match string) string {
		envVar := match[1 : len(match)-1]
		value := os.Getenv(envVar)
		if value == "" {
			return match
		}
		return value
	})

	result = filepath.FromSlash(result)

	return result
}

func (e *Engine) IsAppRunning(appName string) bool {
	appConfig, exists := e.config.Applications[appName]
	if !exists {
		return false
	}

	processNames := appConfig.ProcessNames
	if len(processNames) == 0 {
		processNames = []string{appName}
	}

	for _, processName := range processNames {
		if e.isProcessRunning(processName) {
			return true
		}
	}

	return false
}

func (e *Engine) CreateBackup(sourcePath, backupName string) (string, error) {
	if !e.config.BackupOptions.Enabled {
		return "", nil
	}

	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return "", fmt.Errorf(e.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "SourcePathNotExist", TemplateData: map[string]interface{}{"Path": sourcePath}}))
	}

	timestamp := time.Now().Format("20060102_150405")
	var backupPath string

	if e.config.BackupOptions.Compression {
		backupPath = filepath.Join(e.backupBaseDir, fmt.Sprintf("%s_%s.zip", backupName, timestamp))
		return e.createCompressedBackup(sourcePath, backupPath)
	} else {
		backupPath = filepath.Join(e.backupBaseDir, fmt.Sprintf("%s_%s", backupName, timestamp))
		return e.createDirectoryBackup(sourcePath, backupPath)
	}
}

func (e *Engine) createCompressedBackup(sourcePath, backupPath string) (string, error) {
	zipFile, err := os.Create(backupPath)
	if err != nil {
		return "", err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return "", err
	}

	if fileInfo.IsDir() {
		err = filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			relPath, err := filepath.Rel(sourcePath, path)
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			zipEntry, err := zipWriter.Create(relPath)
			if err != nil {
				return err
			}

			_, err = io.Copy(zipEntry, file)
			return err
		})
	} else {
		file, err := os.Open(sourcePath)
		if err != nil {
			return "", err
		}
		defer file.Close()

		zipEntry, err := zipWriter.Create(filepath.Base(sourcePath))
		if err != nil {
			return "", err
		}

		_, err = io.Copy(zipEntry, file)
		if err != nil {
			return "", err
		}
	}

	if err != nil {
		return "", err
	}

	log.Info().Str("path", backupPath).Msg("Created compressed backup")
	return backupPath, nil
}

func (e *Engine) createDirectoryBackup(sourcePath, backupPath string) (string, error) {
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return "", err
	}

	if fileInfo.IsDir() {
		err = copyDirectory(sourcePath, backupPath)
	} else {
		err = copyFile(sourcePath, backupPath)
	}

	if err != nil {
		return "", err
	}

	log.Info().Str("path", backupPath).Msg("Created directory backup")
	return backupPath, nil
}

func (e *Engine) CleanApplication(ctx context.Context, appName string) error {
	e.sendProgress(ProgressUpdate{
		Type:     "start",
		Message:  e.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "StartReset", TemplateData: map[string]interface{}{"AppName": appName}}),
		AppName:  appName,
		Progress: 0,
	})

	appPath, exists := e.appDataPaths[appName]
	if !exists || appPath == "" {
		return fmt.Errorf(e.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AppNotFound", TemplateData: map[string]interface{}{"AppName": appName}}))
	}

	// Safety checks
	if e.config.SafetyOptions.CheckRunningProcesses {
		if e.IsAppRunning(appName) {
			return fmt.Errorf(e.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AppRunning", TemplateData: map[string]interface{}{"AppName": appName}}))
		}
	}

	// Clean old backups
	e.cleanOldBackups()

	// 初始缓存扫描
	e.sendProgress(ProgressUpdate{
		Type:     "discover",
		Message:  e.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "AnalyzeAppData"}),
		AppName:  appName,
		Progress: 10,
	})

	// 发现缓存信息
	cacheInfo := e.DiscoverCacheInfo(appPath, appName)
	var totalCacheSize int64
	for _, size := range cacheInfo {
		totalCacheSize += size
	}

	e.sendProgress(ProgressUpdate{
		Type:     "discover",
		Message:  e.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "FoundCacheInfo", TemplateData: map[string]interface{}{"Count": len(cacheInfo), "Size": e.FormatSize(totalCacheSize)}}),
		AppName:  appName,
		Progress: 15,
	})

	// Phase 1: Telemetry ID modification
	e.sendProgress(ProgressUpdate{
		Type:     "phase",
		Message:  e.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ModifyingTelemetry"}),
		AppName:  appName,
		Phase:    "telemetry",
		Progress: 20,
	})

	if err := e.modifyTelemetry(appPath, appName); err != nil {
		log.Error().Err(err).Str("app", appName).Msg("Failed to modify telemetry")
	}

	// Phase 2: Database cleaning
	e.sendProgress(ProgressUpdate{
		Type:     "phase",
		Message:  e.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ResettingDatabase"}),
		AppName:  appName,
		Phase:    "database",
		Progress: 50,
	})

	if err := e.cleanDatabases(appPath, appName); err != nil {
		log.Error().Err(err).Str("app", appName).Msg("Failed to clean databases")
	}

	// Phase 3: Cache cleaning
	e.sendProgress(ProgressUpdate{
		Type:     "phase",
		Message:  e.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ResettingCache"}),
		AppName:  appName,
		Phase:    "cache",
		Progress: 80,
	})

	if err := e.cleanCache(appPath, appName); err != nil {
		log.Error().Err(err).Str("app", appName).Msg("Failed to clean cache")
	}

	e.sendProgress(ProgressUpdate{
		Type:     "complete",
		Message:  e.localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ResetSuccess", TemplateData: map[string]interface{}{"AppName": appName}}),
		AppName:  appName,
		Progress: 100,
	})

	return nil
}

// modifyTelemetry modifies telemetry IDs in database and JSON files
func (e *Engine) modifyTelemetry(appPath, appName string) error {
	telemetryKeys := e.config.CleaningOptions.TelemetryKeys
	sessionKeys := e.config.CleaningOptions.SessionKeys
	dbFiles := e.config.CleaningOptions.DatabaseFiles

	// 使用增强的递归文件查找函数
	foundFiles := e.findFilesRecursiveAdvanced(appPath, dbFiles)

	if len(foundFiles) == 0 {
		// 如果没有找到配置的文件，尝试查找所有可能的数据库文件
		foundFiles = e.findDatabaseFiles(appPath)
	}

	// 处理结果统计
	var (
		processedFiles  int
		updatedKeys     int
		deletedKeys     int
		failedFiles     int
		totalFoundFiles = len(foundFiles)
	)

	// 发送开始处理文件的消息
	e.sendProgress(ProgressUpdate{
		Type:     "telemetry",
		Message:  e.localizeMessage("ProcessingStartFiles", map[string]interface{}{"Count": totalFoundFiles}),
		Phase:    "telemetry",
		Progress: 20,
		AppName:  appName,
	})

	// 处理每个找到的文件
	for fileIndex, filePath := range foundFiles {
		// 发送当前处理的文件消息
		progress := 22.0 + float64(fileIndex)*18.0/float64(totalFoundFiles+1)
		e.sendProgress(ProgressUpdate{
			Type: "telemetry",
			Message: e.localizeMessage("ProcessingFile", map[string]interface{}{
				"Current":  fileIndex + 1,
				"Total":    totalFoundFiles,
				"FileName": filepath.Base(filePath),
			}),
			Phase:    "telemetry",
			Progress: progress,
			AppName:  appName,
		})

		// 检查文件是否存在和可访问
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			failedFiles++
			continue
		}

		// 创建备份
		_, err := e.CreateBackup(filePath, fmt.Sprintf("%s_telemetry_%s", appName, filepath.Base(filePath)))
		if err != nil {
			log.Warn().Str("file", filePath).Err(err).Msg("Failed to backup file")
		}

		// 根据文件类型处理
		fileExt := strings.ToLower(filepath.Ext(filePath))
		var fileUpdated, fileSuccess bool
		var fileUpdatedKeys, fileDeletedKeys int

		switch {
		case fileExt == ".vscdb" || fileExt == ".db" || fileExt == ".sqlite" || fileExt == ".sqlite3":
			// 处理SQLite数据库文件
			fileUpdated, fileUpdatedKeys, fileDeletedKeys, fileSuccess = e.processSQLiteFile(filePath, telemetryKeys, sessionKeys)

		case fileExt == ".json":
			fileUpdated, fileUpdatedKeys, fileDeletedKeys, fileSuccess = e.processJSONFile(filePath, telemetryKeys, sessionKeys)

		default:
			continue
		}

		// 更新统计信息
		processedFiles++
		updatedKeys += fileUpdatedKeys
		deletedKeys += fileDeletedKeys
		if !fileSuccess {
			failedFiles++
		}

		// 如果修改成功，记录日志
		if fileUpdated {
			log.Info().Str("file", filePath).Int("updated_keys", fileUpdatedKeys).Int("deleted_keys", fileDeletedKeys).Msg("Successfully modified identifier file")
		}
	}

	// 发送标识符修改完成消息
	e.sendProgress(ProgressUpdate{
		Type: "telemetry",
		Message: e.localizeMessage("TelemetryModificationComplete", map[string]interface{}{
			"Processed": processedFiles,
			"Updated":   updatedKeys,
			"Deleted":   deletedKeys,
			"Failed":    failedFiles,
		}),
		Phase:    "telemetry",
		Progress: 45,
		AppName:  appName,
	})

	return nil
}

// processSQLiteFile 处理单个SQLite文件，返回是否更新成功，更新的键数，删除的键数，以及处理是否成功
func (e *Engine) processSQLiteFile(dbPath string, telemetryKeys, sessionKeys []string) (bool, int, int, bool) {

	// 尝试使用不同的连接参数打开数据库
	connectionStrings := []string{
		dbPath + "?_journal=WAL&_timeout=5000",
		dbPath + "?mode=rw",
		dbPath, // 简单连接，作为最后尝试
	}

	for _, connStr := range connectionStrings {
		db, err := sql.Open("sqlite", connStr)
		if err != nil {
			log.Debug().Str("connection", connStr).Err(err).Msg("Failed to open database connection")
			continue
		}
		defer db.Close()

		// 检查数据库连接
		if err := db.Ping(); err != nil {
			log.Debug().Str("connection", connStr).Err(err).Msg("Failed to ping database")
			continue
		}

		log.Debug().Str("connection", connStr).Msg("Successfully connected to database")

		// 查找ItemTable或类似表
		tables, err := e.findRelevantTables(db)
		if err != nil {
			log.Error().Err(err).Msg("Failed to find relevant tables")
			continue
		}

		if len(tables) == 0 {
			log.Warn().Msg("No processable tables found in database")
			return false, 0, 0, true // 没有表不算失败
		}

		// 开始事务
		tx, err := db.Begin()
		if err != nil {
			log.Error().Err(err).Msg("Failed to begin transaction")
			continue
		}

		// 生成新ID
		newMachineID := uuid.New().String()
		newSessionID := uuid.New().String()

		totalUpdatedKeys := 0
		totalDeletedKeys := 0

		// 处理每个相关表
		for _, tableInfo := range tables {
			tableName := tableInfo.name
			keyColumn := tableInfo.keyColumn
			valueColumn := tableInfo.valueColumn

			// 更新telemetry keys
			for _, key := range telemetryKeys {
				value := newMachineID
				if strings.Contains(strings.ToLower(key), "session") {
					value = newSessionID
				}

				// 安全构造SQL语句
				updateSQL := fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ?",
					quoteIdentifier(tableName),
					quoteIdentifier(valueColumn),
					quoteIdentifier(keyColumn))

				result, err := tx.Exec(updateSQL, value, key)
				if err != nil {
					log.Debug().Str("table", tableName).Str("key", key).Err(err).Msg("Failed to update key")
					continue
				}

				if affected, err := result.RowsAffected(); err == nil && affected > 0 {
					totalUpdatedKeys++
					log.Debug().Str("table", tableName).Str("key", key).Msg("Successfully updated key")
				}
			}

			// 删除session keys
			for _, key := range sessionKeys {
				deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE %s = ?",
					quoteIdentifier(tableName),
					quoteIdentifier(keyColumn))

				result, err := tx.Exec(deleteSQL, key)
				if err != nil {
					log.Debug().Str("table", tableName).Str("key", key).Err(err).Msg("Failed to delete key")
					continue
				}

				if affected, err := result.RowsAffected(); err == nil && affected > 0 {
					totalDeletedKeys++
					log.Debug().Str("table", tableName).Str("key", key).Msg("Successfully deleted key")
				}
			}
		}

		// 提交事务
		if err := tx.Commit(); err != nil {
			log.Error().Err(err).Msg("Failed to commit transaction")
			return false, 0, 0, false
		}

		// 如果有更改，执行VACUUM
		if totalUpdatedKeys > 0 || totalDeletedKeys > 0 {
			if _, err := db.Exec("VACUUM"); err != nil {
				log.Warn().Err(err).Msg("Failed to execute VACUUM")
				// 继续处理，不返回错误
			}
			return true, totalUpdatedKeys, totalDeletedKeys, true
		}

		return false, 0, 0, true // 没有更改，��成功处理
	}

	// 所有连接方式都失败
	return false, 0, 0, false
}

// TableInfo 表示数据库表的结构信息
type TableInfo struct {
	name        string
	keyColumn   string
	valueColumn string
}

// findRelevantTables 查找可能包含配置或设置的表
func (e *Engine) findRelevantTables(db *sql.DB) ([]TableInfo, error) {
	var tables []TableInfo

	// 获取所有表
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 收集表名
	var tableNames []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}
		tableNames = append(tableNames, tableName)
	}

	// 优先检查常见的配置表名
	priorityTables := []string{"ItemTable", "Settings", "Preferences", "Config", "Configuration"}
	for _, priorityTable := range priorityTables {
		if contains(tableNames, priorityTable) {
			tableInfo, found := e.analyzeTableStructure(db, priorityTable)
			if found {
				tables = append(tables, tableInfo)
			}
		}
	}

	// 如果没有找到优先表，检查所有表
	if len(tables) == 0 {
		for _, tableName := range tableNames {
			// 跳过系统表
			if strings.HasPrefix(tableName, "sqlite_") {
				continue
			}

			tableInfo, found := e.analyzeTableStructure(db, tableName)
			if found {
				tables = append(tables, tableInfo)
			}
		}
	}

	return tables, nil
}

// analyzeTableStructure 分析表结构，寻找key-value对
func (e *Engine) analyzeTableStructure(db *sql.DB, tableName string) (TableInfo, bool) {
	// 获取表结构
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoteIdentifier(tableName)))
	if err != nil {
		return TableInfo{}, false
	}
	defer rows.Close()

	// 列名映射
	var columns []string
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, dfltValue, pk interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			continue
		}
		columns = append(columns, name)
	}

	// 查找可能的key和value列
	var keyCol, valueCol string

	// 首先尝试完全匹配
	if contains(columns, "key") && contains(columns, "value") {
		return TableInfo{tableName, "key", "value"}, true
	}

	// 然后尝试部分匹配
	for _, col := range columns {
		lowerCol := strings.ToLower(col)
		if strings.Contains(lowerCol, "key") ||
			strings.Contains(lowerCol, "name") ||
			strings.Contains(lowerCol, "id") ||
			strings.Contains(lowerCol, "setting") {
			keyCol = col
		} else if strings.Contains(lowerCol, "value") ||
			strings.Contains(lowerCol, "data") ||
			strings.Contains(lowerCol, "content") {
			valueCol = col
		}
	}

	// 如果找到了key和value列
	if keyCol != "" && valueCol != "" {
		return TableInfo{tableName, keyCol, valueCol}, true
	}

	return TableInfo{}, false
}

// processJSONFile 处理单个JSON文件，返回是否更新成功，更新的键数，删除的键数，以及处理是否成功
func (e *Engine) processJSONFile(jsonPath string, telemetryKeys, sessionKeys []string) (bool, int, int, bool) {

	// 1. 创建备份副本以便出错时恢复
	tempBackupPath := jsonPath + ".bak"
	err := copyFile(jsonPath, tempBackupPath)
	if err != nil {
		// 继续处理，即使没有备份
	} else {
		defer func() {
			// 如果成功，删除临时备份
			os.Remove(tempBackupPath)
		}()
	}

	// 2. 读取JSON文件，使用更安全的方式
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		log.Error().Str("path", jsonPath).Err(err).Msg("读取JSON文件失败")
		return false, 0, 0, false
	}

	// 3. 处理空文件的情况
	if len(data) == 0 {
		return false, 0, 0, true // 视为成功处理但无需更改
	}

	// 4. 解析JSON，支持不同的JSON结构
	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		// 尝试作为JSON数组解析
		var jsonArray []interface{}
		if err2 := json.Unmarshal(data, &jsonArray); err2 != nil {
			log.Error().Str("path", jsonPath).Err(err).Msg("解析JSON失败")
			return false, 0, 0, false
		}

		// 不支持处理JSON数组
		return false, 0, 0, true // 视为成功处理但无需更改
	}

	// 5. 生成新ID
	newMachineID := uuid.New().String()
	newSessionID := uuid.New().String()

	// 6. 更新统计
	updatedKeys := 0
	deletedKeys := 0
	modified := false

	// 7. 处理包括嵌套结构的JSON
	processNestedJSON(jsonData, telemetryKeys, sessionKeys, newMachineID, newSessionID, &updatedKeys, &deletedKeys, &modified)

	// 8. 如果有修改，写回文件
	if modified {
		// 使用更美观的缩进格式
		newData, err := json.MarshalIndent(jsonData, "", "  ")
		if err != nil {
			log.Error().Str("path", jsonPath).Err(err).Msg("JSON序列化失败")
			// 尝试恢复备份
			if _, err := os.Stat(tempBackupPath); err == nil {
				if restoreErr := copyFile(tempBackupPath, jsonPath); restoreErr != nil {
					log.Error().Str("path", jsonPath).Err(restoreErr).Msg("恢复备份失败")
				} else {
					log.Info().Str("path", jsonPath).Msg("已从备份恢复原始文件")
				}
			}
			return false, 0, 0, false
		}

		// 写入文件，保持原始文件权限
		fileInfo, err := os.Stat(jsonPath)
		if err != nil {
			log.Warn().Str("path", jsonPath).Err(err).Msg("获取文件权限失败，使用默认权限")
		}

		// 使用临时文件并重命名的方式写入，避免文件损坏
		tempFilePath := jsonPath + ".tmp"
		err = os.WriteFile(tempFilePath, newData, 0644)
		if err != nil {
			log.Error().Str("path", tempFilePath).Err(err).Msg("写入临时文件失败")
			// 尝试删除临时文件
			os.Remove(tempFilePath)
			// 尝试恢复备份
			if _, err := os.Stat(tempBackupPath); err == nil {
				if restoreErr := copyFile(tempBackupPath, jsonPath); restoreErr != nil {
					log.Error().Str("path", jsonPath).Err(restoreErr).Msg("恢复备份失败")
				} else {
					log.Info().Str("path", jsonPath).Msg("已从备份恢复原始文件")
				}
			}
			return false, 0, 0, false
		}

		// 如果有获取到原始权限，则设置相同的权限
		if fileInfo != nil {
			if err := os.Chmod(tempFilePath, fileInfo.Mode()); err != nil {
				log.Warn().Str("path", tempFilePath).Err(err).Msg("设置文件权限失败")
				// 继续处理，不视为致命错误
			}
		}

		// 重命名临时文件，替换原始文件
		if err := os.Rename(tempFilePath, jsonPath); err != nil {
			log.Error().Str("from", tempFilePath).Str("to", jsonPath).Err(err).Msg("重命名文件失败")
			// 尝试删除临时文件
			os.Remove(tempFilePath)
			// 尝试恢复备份
			if _, err := os.Stat(tempBackupPath); err == nil {
				if restoreErr := copyFile(tempBackupPath, jsonPath); restoreErr != nil {
					log.Error().Str("path", jsonPath).Err(restoreErr).Msg("恢复备份失败")
				} else {
					log.Info().Str("path", jsonPath).Msg("已从备份恢复原始文件")
				}
			}
			return false, 0, 0, false
		}

		log.Info().
			Str("path", jsonPath).
			Int("updated_keys", updatedKeys).
			Int("deleted_keys", deletedKeys).
			Msg("成功更新JSON文件")
		return true, updatedKeys, deletedKeys, true
	}

	return false, 0, 0, true // 没有更改，但处理成功
}

// processNestedJSON 递归处理嵌套的JSON结构
func processNestedJSON(data map[string]interface{}, telemetryKeys, sessionKeys []string,
	newMachineID, newSessionID string,
	updatedKeys, deletedKeys *int, modified *bool) {
	// 处理当前级别的键
	for _, key := range telemetryKeys {
		if val, exists := data[key]; exists {
			// 检查是否为string类型的值
			if _, isString := val.(string); isString {
				if strings.Contains(strings.ToLower(key), "session") {
					data[key] = newSessionID
				} else {
					data[key] = newMachineID
				}
				*modified = true
				*updatedKeys++
			}
		}
	}

	// 处理会话键
	for _, key := range sessionKeys {
		if _, exists := data[key]; exists {
			delete(data, key)
			*modified = true
			*deletedKeys++
		}
	}

	// 递归处理嵌套的对象
	for _, val := range data {
		// 如果值是一个嵌套的map
		if nestedMap, isMap := val.(map[string]interface{}); isMap {
			processNestedJSON(nestedMap, telemetryKeys, sessionKeys, newMachineID, newSessionID, updatedKeys, deletedKeys, modified)
		} else if nestedArray, isArray := val.([]interface{}); isArray {
			// 如果值是一个数组
			for _, item := range nestedArray {
				// 如果数组元素是一个map
				if nestedItem, isMap := item.(map[string]interface{}); isMap {
					processNestedJSON(nestedItem, telemetryKeys, sessionKeys, newMachineID, newSessionID, updatedKeys, deletedKeys, modified)
				}
			}
		}
	}
}

// cleanDatabases cleans database files
func (e *Engine) cleanDatabases(appPath, appName string) error {
	log.Info().Str("app", appName).Str("path", appPath).Msg("开始重置数据库")

	// 首先查找所有数据库文件
	dbFiles := e.findDatabaseFiles(appPath)
	totalFiles := len(dbFiles)

	if totalFiles == 0 {
		log.Warn().Str("app", appName).Msg("没有找到数据库文件")
		e.sendProgress(ProgressUpdate{
			Type:     "database",
			Message:  e.localizeMessage("NoDatabaseFound", map[string]interface{}{}),
			AppName:  appName,
			Phase:    "database",
			Progress: 65,
		})
		return nil
	}

	e.sendProgress(ProgressUpdate{
		Type:     "database",
		Message:  e.localizeMessage("FoundDatabases", map[string]interface{}{"Count": totalFiles}),
		AppName:  appName,
		Phase:    "database",
		Progress: 50,
	})

	// 跟踪处理结果
	var (
		processedFiles int
		cleanedFiles   int
		totalRecords   int
		failedFiles    int
	)

	keywords := e.config.CleaningOptions.DatabaseKeywords

	// 处理每个数据库文件
	for fileIndex, dbPath := range dbFiles {
		progress := 50.0 + float64(fileIndex)*15.0/float64(totalFiles+1)
		e.sendProgress(ProgressUpdate{
			Type: "database",
			Message: e.localizeMessage("ProcessingDatabase", map[string]interface{}{
				"Current":  fileIndex + 1,
				"Total":    totalFiles,
				"FileName": filepath.Base(dbPath),
			}),
			AppName:  appName,
			Phase:    "database",
			Progress: progress,
		})

		// 检查是否是备份文件
		if strings.Contains(strings.ToLower(dbPath), "backup") || strings.Contains(dbPath, ".bak") {
			log.Debug().Str("path", dbPath).Msg("跳过备份文件")
			continue
		}

		// 创建备份
		backupPath, err := e.CreateBackup(dbPath, fmt.Sprintf("%s_database_%s", appName, filepath.Base(dbPath)))
		if err != nil {
			log.Warn().Str("file", dbPath).Err(err).Msg("备份数据库失败，继续处理")
		} else {
			log.Info().Str("file", dbPath).Str("backup", backupPath).Msg("成功创建数据库备份")
		}

		// 重置数据库
		cleaned, recordsAffected, success := e.cleanSQLiteDatabaseAdvanced(dbPath, keywords)

		// 更新统计
		processedFiles++
		if cleaned {
			cleanedFiles++
			totalRecords += recordsAffected
		}
		if !success {
			failedFiles++
		}

		if cleaned {
			log.Info().Str("file", dbPath).Int("records_affected", recordsAffected).Msg("成功重置数据库")
		}
	}

	// 发送完成消息
	e.sendProgress(ProgressUpdate{
		Type: "database",
		Message: e.localizeMessage("DatabaseResetComplete", map[string]interface{}{
			"Reset":   cleanedFiles,
			"Total":   processedFiles,
			"Records": totalRecords,
			"Failed":  failedFiles,
		}),
		AppName:  appName,
		Phase:    "database",
		Progress: 65,
	})

	return nil
}

// cleanSQLiteDatabaseAdvanced 增强版的SQLite数据库重置函数
func (e *Engine) cleanSQLiteDatabaseAdvanced(dbPath string, keywords []string) (bool, int, bool) {

	// 尝试使用不同的连接参数打开数据库
	connectionStrings := []string{
		dbPath + "?_journal=WAL&_timeout=5000",
		dbPath + "?mode=rw",
		dbPath, // 简单连接，作为最后尝试
	}

	for _, connStr := range connectionStrings {
		db, err := sql.Open("sqlite", connStr)
		if err != nil {
			log.Debug().Str("connection", connStr).Err(err).Msg("尝试连接数据库失败")
			continue
		}
		defer db.Close()

		// 检查数据库连接
		if err := db.Ping(); err != nil {
			log.Debug().Str("connection", connStr).Err(err).Msg("Ping数据库失败")
			continue
		}

		log.Debug().Str("connection", connStr).Msg("成功连接到数据库")

		// 开始事务
		tx, err := db.Begin()
		if err != nil {
			log.Error().Err(err).Msg("开始事务失败")
			continue
		}

		// 获取所有表
		tables, err := tx.Query("SELECT name FROM sqlite_master WHERE type='table'")
		if err != nil {
			log.Error().Err(err).Msg("获取表列表失败")
			tx.Rollback()
			continue
		}

		var tableNames []string
		for tables.Next() {
			var tableName string
			if err := tables.Scan(&tableName); err != nil {
				log.Warn().Err(err).Msg("读取表名失败")
				continue
			}
			// 跳过系统表
			if !strings.HasPrefix(tableName, "sqlite_") {
				tableNames = append(tableNames, tableName)
			}
		}
		tables.Close()

		if len(tableNames) == 0 {
			log.Warn().Str("path", dbPath).Msg("数据库中没有找到用户表")
			tx.Rollback()
			return false, 0, true // 没有表不算失败
		}

		cleanedRecords := 0
		cachePatterns := e.config.CleaningOptions.CacheTablePatterns

		// 首先重置缓存表（完全删除）
		for _, tableName := range tableNames {
			// 检查表名是否安全
			if !isValidTableName(tableName) {
				log.Warn().Str("table", tableName).Msg("跳过不安全的表名")
				continue
			}

			// 查找匹配缓存模式的表
			for _, pattern := range cachePatterns {
				if strings.Contains(strings.ToLower(tableName), pattern) {
					log.Debug().Str("table", tableName).Str("pattern", pattern).Msg("重置缓存表")

					// 清空整个表
					deleteSql := fmt.Sprintf("DELETE FROM %s", quoteIdentifier(tableName))
					result, err := tx.Exec(deleteSql)
					if err != nil {
						log.Warn().Str("table", tableName).Err(err).Msg("清空表失败")
						continue
					}

					if affected, err := result.RowsAffected(); err == nil && affected > 0 {
						cleanedRecords += int(affected)
						log.Info().Str("table", tableName).Int64("records", affected).Msg("清空表成功")
					}
					break
				}
			}
		}

		// 然后处理其他表，按关键词重置
		for _, tableName := range tableNames {
			// 检查表名是否安全
			if !isValidTableName(tableName) {
				continue
			}

			// 获取表的所有列
			columnSQL := fmt.Sprintf("PRAGMA table_info(%s)", quoteIdentifier(tableName))
			colRows, err := tx.Query(columnSQL)
			if err != nil {
				log.Warn().Str("table", tableName).Err(err).Msg("获取表列信息失败")
				continue
			}

			var columns []string
			for colRows.Next() {
				var cid int
				var name, ctype string
				var notnull, dfltValue, pk interface{}
				if err := colRows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
					continue
				}
				// 只处理安全的列名
				if isValidColumnName(name) {
					columns = append(columns, name)
				}
			}
			colRows.Close()

			// 对每个列和关键词组合尝试删除记录
			for _, keyword := range keywords {
				for _, column := range columns {
					// 尝试查找包含关键词的记录
					deleteSql := fmt.Sprintf("DELETE FROM %s WHERE %s LIKE ?",
						quoteIdentifier(tableName),
						quoteIdentifier(column))
					result, err := tx.Exec(deleteSql, "%"+keyword+"%")
					if err != nil {
						log.Debug().Str("table", tableName).Str("column", column).Str("keyword", keyword).Err(err).Msg("按关键词删除记录失败")
						continue
					}

					if affected, err := result.RowsAffected(); err == nil && affected > 0 {
						cleanedRecords += int(affected)
						log.Info().Str("table", tableName).Str("column", column).Str("keyword", keyword).Int64("records", affected).Msg("按关键词删除记录成功")
					}
				}
			}

			// 检查通用的用户/账户列
			userColumns := []string{"user_id", "account_id", "email", "username", "userid", "accountid"}
			for _, column := range columns {
				columnLower := strings.ToLower(column)
				for _, userCol := range userColumns {
					if columnLower == userCol || strings.Contains(columnLower, userCol) {
						log.Debug().Str("table", tableName).Str("column", column).Msg("尝试重置用户相关列")

						// 尝试将字段设为NULL或空值
						updateSql := fmt.Sprintf("UPDATE %s SET %s = NULL WHERE %s IS NOT NULL",
							quoteIdentifier(tableName),
							quoteIdentifier(column),
							quoteIdentifier(column))
						result, err := tx.Exec(updateSql)
						if err != nil {
							log.Debug().Str("table", tableName).Str("column", column).Err(err).Msg("设置列为NULL失败，尝试清空")

							// 尝试清空值
							updateSql = fmt.Sprintf("UPDATE %s SET %s = '' WHERE %s != ''",
								quoteIdentifier(tableName),
								quoteIdentifier(column),
								quoteIdentifier(column))
							result, err = tx.Exec(updateSql)
							if err != nil {
								log.Debug().Str("table", tableName).Str("column", column).Err(err).Msg("清空列值失败")
								continue
							}
						}

						if affected, err := result.RowsAffected(); err == nil && affected > 0 {
							cleanedRecords += int(affected)
							log.Info().Str("table", tableName).Str("column", column).Int64("records", affected).Msg("重置用户相关列成功")
						}
					}
				}
			}
		}

		// 提交事务
		if err := tx.Commit(); err != nil {
			log.Error().Err(err).Msg("提交事务失败")
			tx.Rollback()
			return false, 0, false
		}

		// 如果有重置的记录，优化数据库
		if cleanedRecords > 0 {
			log.Info().Str("path", dbPath).Msg("优化数据库")
			if _, err := db.Exec("VACUUM"); err != nil {
				log.Warn().Err(err).Msg("执行VACUUM失败")
				// 继续处理，不返回错误
			}
			return true, cleanedRecords, true
		}

		return false, 0, true // 没有记录需要重置，但处理成功
	}

	// 所有连接方式都失败
	return false, 0, false
}

// isValidTableName 检查表名是否安全有效
func isValidTableName(name string) bool {
	// 表名只能包含字母、数字、下划线，且不能以数字开头
	validPattern := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	return validPattern.MatchString(name)
}

// isValidColumnName 检查列名是否安全有效
func isValidColumnName(name string) bool {
	// 列名只能包含字母、数字、下划线，且不能以数字开头
	validPattern := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	return validPattern.MatchString(name)
}

// quoteIdentifier 安全地引用SQL标识符
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// getTableColumns gets column names for a table
func (e *Engine) getTableColumns(db *sql.DB, tableName string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ, dfltValue string
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			continue
		}
		columns = append(columns, name)
	}

	return columns, nil
}

// cleanCache cleans cache directories
func (e *Engine) cleanCache(appPath, appName string) error {
	cacheDirs := e.config.CleaningOptions.CacheDirectories

	e.sendProgress(ProgressUpdate{
		Type:     "cache",
		Message:  e.localizeMessage("SearchingCacheDirectories", map[string]interface{}{"Count": len(cacheDirs)}),
		AppName:  appName,
		Phase:    "cache",
		Progress: 80,
	})

	// 为每个缓存目录类型创建统计信息
	stats := make(map[string]*CacheStats)
	for _, dir := range cacheDirs {
		stats[dir] = &CacheStats{}
	}

	// 递归查找所有缓存目录
	allFoundDirs := []string{}

	// 使用更精确的搜索方法
	for dirIndex, dirName := range cacheDirs {
		progress := 80.0 + float64(dirIndex)*5.0/float64(len(cacheDirs))

		e.sendProgress(ProgressUpdate{
			Type:     "cache",
			Message:  e.localizeMessage("SearchingCacheDirectory", map[string]interface{}{"DirName": dirName}),
			AppName:  appName,
			Phase:    "cache",
			Progress: progress,
		})

		// 查找匹配的目录
		foundDirs := e.findDirectoriesRecursive(appPath, []string{dirName})

		if len(foundDirs) > 0 {
			log.Info().Int("count", len(foundDirs)).Str("dir_type", dirName).Msgf("Found %d %s directories", len(foundDirs), dirName)
			for _, dir := range foundDirs {
				log.Debug().Str("type", dirName).Str("path", dir).Msg("Found cache directory")
			}
		} else {
			log.Debug().Str("type", dirName).Msg("No directories found")
		}

		allFoundDirs = append(allFoundDirs, foundDirs...)

		stats[dirName].DirCount = len(foundDirs)

		// 计算每个目录类型的总大小
		for _, dir := range foundDirs {
			size := e.GetDirectorySize(dir)
			stats[dirName].TotalSize += size
		}
	}

	// 如果没有找到任何缓存目录，返回提示信息
	if len(allFoundDirs) == 0 {
		log.Warn().Str("app", appName).Str("path", appPath).Msg("No cache directories found")
		e.sendProgress(ProgressUpdate{
			Type:     "cache",
			Message:  e.localizeMessage("NoCacheFound", map[string]interface{}{}),
			AppName:  appName,
			Phase:    "cache",
			Progress: 100,
		})
		return nil
	}

	// 开始重置缓存目录
	e.sendProgress(ProgressUpdate{
		Type:     "cache",
		Message:  e.localizeMessage("StartingCacheReset", map[string]interface{}{"Count": len(allFoundDirs)}),
		AppName:  appName,
		Phase:    "cache",
		Progress: 85,
	})

	// 按目录类型重置缓存
	for dirIndex, dirName := range cacheDirs {
		foundDirs := e.findDirectoriesRecursive(appPath, []string{dirName})
		if len(foundDirs) == 0 {
			continue
		}

		e.sendProgress(ProgressUpdate{
			Type: "cache",
			Message: e.localizeMessage("CacheDirectoryFound", map[string]interface{}{
				"DirName": dirName,
				"Count":   len(foundDirs),
			}),
			AppName:  appName,
			Phase:    "cache",
			Progress: 85 + float64(dirIndex)*10.0/float64(len(cacheDirs)*2),
		})

		for i, dir := range foundDirs {
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				continue
			}

			subProgress := 85 + float64(dirIndex)*10.0/float64(len(cacheDirs)) +
				float64(i)*5.0/float64(len(foundDirs)*len(cacheDirs))

			e.sendProgress(ProgressUpdate{
				Type: "cache",
				Message: e.localizeMessage("ResettingCacheDirectory", map[string]interface{}{
					"DirName":     dirName,
					"Current":     i + 1,
					"Total":       len(foundDirs),
					"DirBaseName": filepath.Base(dir),
				}),
				AppName:  appName,
				Phase:    "cache",
				Progress: subProgress,
			})

			sizeBefore := e.GetDirectorySize(dir)

			// 创建备份
			backupName := fmt.Sprintf("%s_cache_%s", appName, strings.ReplaceAll(filepath.Base(dir), "/", "_"))
			_, err := e.CreateBackup(dir, backupName)
			if err != nil {
				log.Warn().Str("dir", dir).Err(err).Msg("Failed to create backup")
			}

			// 清空目录内容
			if e.dryRun {
				log.Info().Str("dir", dir).Str("size", e.FormatSize(sizeBefore)).Msg("Would clear cache directory")
			} else {
				if err := e.clearDirectoryContents(dir); err != nil {
					log.Error().Str("dir", dir).Err(err).Msg("Failed to clear cache directory")
				} else {
					stats[dirName].CleanedDirs++
					log.Info().
						Str("dir", dir).
						Str("size_freed", e.FormatSize(sizeBefore)).
						Msg("Cleared cache directory")
				}

				// 验证重置结果
				sizeAfter := e.GetDirectorySize(dir)
				if sizeAfter > 0 {
					log.Warn().
						Str("dir", dir).
						Str("remaining_size", e.FormatSize(sizeAfter)).
						Msg("Directory not completely cleared")

					// 尝试再次重置
					log.Info().Str("dir", dir).Msg("Attempting second cleanup pass")
					if err := e.clearDirectoryContents(dir); err != nil {
						log.Error().Str("dir", dir).Err(err).Msg("Failed second cleanup attempt")
					} else {
						finalSize := e.GetDirectorySize(dir)
						if finalSize < sizeAfter {
							log.Info().
								Str("dir", dir).
								Str("before", e.FormatSize(sizeAfter)).
								Str("after", e.FormatSize(finalSize)).
								Msg("Second cleanup pass improved results")
						}
					}
				}
			}
		}
	}

	// 生成并记录总结报告
	var totalSize int64
	var totalCleanedDirs int

	for dirName, stat := range stats {
		if stat.DirCount > 0 {
			log.Info().
				Str("directory_type", dirName).
				Int("directories", stat.DirCount).
				Int("cleaned", stat.CleanedDirs).
				Str("size_freed", e.FormatSize(stat.TotalSize)).
				Msgf("Cache stats: %s", dirName)

			totalSize += stat.TotalSize
			totalCleanedDirs += stat.CleanedDirs
		}
	}

	log.Info().
		Str("app", appName).
		Int("directories_cleaned", totalCleanedDirs).
		Str("total_size_freed", e.FormatSize(totalSize)).
		Msg("Total cache cleaning results")

	// 发送最终的完成进度
	e.sendProgress(ProgressUpdate{
		Type: "cache",
		Message: e.localizeMessage("CacheResetComplete", map[string]interface{}{
			"DirCount": totalCleanedDirs,
			"Size":     e.FormatSize(totalSize),
		}),
		AppName:  appName,
		Phase:    "cache",
		Progress: 100,
	})

	return nil
}

// clearDirectoryContents clears all contents of a directory
func (e *Engine) clearDirectoryContents(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", directory, err)
	}

	var failedItems []string
	var removedFiles, removedDirs int

	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())

		// 尝试获取文件信息，但如果失败也继续处理
		info, err := entry.Info()
		if err != nil {
			log.Debug().Str("path", path).Err(err).Msg("Failed to get file info, will try to remove anyway")
			// 即使获取信息失败，也尝试删除
			if err := os.RemoveAll(path); err != nil {
				log.Warn().Str("path", path).Err(err).Msg("Failed to remove item")
				failedItems = append(failedItems, path)
			}
			continue
		}

		// 处理符号链接
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(path); err != nil {
				log.Warn().Str("path", path).Err(err).Msg("Failed to remove symlink")
				failedItems = append(failedItems, path)
			} else {
				removedFiles++
			}
			continue
		}

		// 处理普通文件和目录
		if info.IsDir() {
			// 对于目录，先尝试清空内容再删除
			subEntries, err := os.ReadDir(path)
			if err == nil && len(subEntries) > 0 {
				// 如果目录不为空，先递归清空
				if subErr := e.clearDirectoryContents(path); subErr != nil {
					log.Debug().
						Str("path", path).
						Err(subErr).
						Msg("Failed to clear subdirectory contents, will try to remove entire directory")
				}
			}

			// 无论上面的递归清空是否成功，都尝试删除整个目录
			if err := os.RemoveAll(path); err != nil {
				log.Warn().Str("path", path).Err(err).Msg("Failed to remove directory")
				failedItems = append(failedItems, path)
			} else {
				removedDirs++
			}
		} else {
			// 对于文件，尝试多种删除方法
			if err := os.Remove(path); err != nil {
				// 如果普通删除失败，尝试更改权限后再删除
				os.Chmod(path, 0666) // 尝试更改权限
				if err := os.Remove(path); err != nil {
					log.Warn().Str("path", path).Err(err).Msg("Failed to remove file even after chmod")
					failedItems = append(failedItems, path)
				} else {
					removedFiles++
				}
			} else {
				removedFiles++
			}
		}
	}

	log.Debug().
		Str("dir", directory).
		Int("removed_files", removedFiles).
		Int("removed_dirs", removedDirs).
		Int("failed_items", len(failedItems)).
		Msg("Directory cleaning results")

	// 即使有失败项，也返回成功，以便继续处理其他目录
	if len(failedItems) > 0 {
		log.Warn().
			Str("directory", directory).
			Int("failed_count", len(failedItems)).
			Strs("first_few", failedItems[:min(3, len(failedItems))]).
			Msg("Some items could not be removed")
	}

	return nil
}

// GetDirectorySize calculates the total size of a directory
func (e *Engine) GetDirectorySize(directory string) int64 {
	var totalSize int64

	filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})

	return totalSize
}

// FormatSize formats file size in human readable format
func (e *Engine) FormatSize(sizeBytes int64) string {
	const unit = 1024
	if sizeBytes < unit {
		return fmt.Sprintf("%d B", sizeBytes)
	}
	div, exp := int64(unit), 0
	for n := sizeBytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(sizeBytes)/float64(div), "KMGTPE"[exp])
}

// findFilesRecursive finds files recursively
func (e *Engine) findFilesRecursive(root string, filenames []string) []string {
	var found []string

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			for _, filename := range filenames {
				if filepath.Base(path) == filename {
					found = append(found, path)
					break
				}
			}
		}
		return nil
	})

	return found
}

// findDirectoriesRecursive 递归查找指定名称的目录
func (e *Engine) findDirectoriesRecursive(root string, dirNames []string) []string {
	var found []string

	log.Debug().Str("root", root).Strs("targets", dirNames).Msg("Searching for directories")

	// 使用更强大的递归方法
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Debug().Str("path", path).Err(err).Msg("Error accessing path")
			return nil // 跳过错误，继续搜索
		}

		if !info.IsDir() {
			return nil // 跳过非目录
		}

		// 获取当前目录名称
		baseName := filepath.Base(path)

		// 检查是否匹配任何目标目录名称
		for _, dirName := range dirNames {
			// 处理包含斜杠的路径，例如"User/workspaceStorage"
			if strings.Contains(dirName, "/") {
				// 分割路径
				parts := strings.Split(dirName, "/")
				lastPart := parts[len(parts)-1]

				// 检查是否为最后一部分
				if baseName == lastPart {
					// 检查父路径是否包含前面的部分
					parentPath := filepath.Dir(path)
					parentName := filepath.Base(parentPath)

					// 如果父��录名称匹配第一部分，或者路径中包含第一部分
					if parentName == parts[0] || strings.Contains(path, parts[0]) {
						log.Debug().Str("path", path).Str("dirName", dirName).Str("baseName", baseName).Str("parentName", parentName).Msg("Found matching directory with parent path")
						found = append(found, path)
						break
					}
				}
			} else if baseName == dirName {
				// 直接匹配目录名
				log.Debug().Str("path", path).Str("dirName", dirName).Msg("Found matching directory")
				found = append(found, path)
				break
			}
		}

		return nil
	})

	log.Debug().Int("count", len(found)).Strs("dirs", found).Msg("Directory search results")
	return found
}

// findFilesRecursiveAdvanced 高级递归查找文件的函数
func (e *Engine) findFilesRecursiveAdvanced(root string, filenames []string) []string {
	log.Debug().Str("root", root).Strs("targets", filenames).Msg("Starting recursive file search")

	var found []string
	var totalFiles int

	// 创建文件名查找映射，提高匹配效率
	filenameMap := make(map[string]bool)
	for _, name := range filenames {
		filenameMap[strings.ToLower(name)] = true
	}

	// 使用filepath.Walk递归查找所有文件
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Debug().Str("path", path).Err(err).Msg("Error accessing path")
			return nil // 继续处理其他路径
		}

		totalFiles++

		// 仅处理文件，不处理目录
		if !info.IsDir() {
			baseName := filepath.Base(path)

			// 尝试直接匹配
			if filenameMap[strings.ToLower(baseName)] {
				log.Debug().Str("path", path).Msg("Found matching file")
				found = append(found, path)
			}
		}

		return nil
	})

	if err != nil {
		log.Error().Err(err).Msg("Error during file recursion")
	}

	log.Info().Str("root", root).Int("total_files_scanned", totalFiles).Int("matches_found", len(found)).Msg("File recursion finished")

	return found
}

// findDatabaseFiles 专门查找数据库文件
func (e *Engine) findDatabaseFiles(root string) []string {
	log.Debug().Str("root", root).Msg("Searching for database files")

	var found []string
	var totalFiles int

	// 数据库文件扩展名
	dbExtensions := []string{".vscdb", ".db", ".sqlite", ".sqlite3"}

	// 创建扩展名查找映射，提高匹配效率
	extMap := make(map[string]bool)
	for _, ext := range dbExtensions {
		extMap[ext] = true
	}

	// 使用filepath.Walk递归查找所有文件
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 继续处理其他路径
		}

		totalFiles++

		// 仅处理文件，不处理目录
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))

			// 检查是否为数据库文件
			if extMap[ext] {
				// 检查是否为备份文件
				if !strings.Contains(strings.ToLower(path), "backup") &&
					!strings.Contains(path, ".bak") {
					found = append(found, path)
				}
			}
		}

		return nil
	})

	log.Info().Str("root", root).Int("total_files_scanned", totalFiles).Int("matches_found", len(found)).Msg("Database file search finished")

	return found
}

// cleanOldBackups cleans old backups based on retention policy
func (e *Engine) cleanOldBackups() {
	retentionDays := e.config.BackupOptions.RetentionDays
	if retentionDays <= 0 {
		return
	}

	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

	entries, err := os.ReadDir(e.backupBaseDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoffTime) {
			path := filepath.Join(e.backupBaseDir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				log.Warn().Str("path", path).Err(err).Msg("Failed to remove old backup")
			} else {
				log.Info().Str("path", path).Msg("Removed old backup")
			}
		}
	}
}

// localizeMessage 使用国际化键和模板数据生成本地化消息
func (e *Engine) localizeMessage(messageID string, templateData map[string]interface{}) string {
	return e.localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: templateData,
	})
}

// sendProgress sends a progress update
func (e *Engine) sendProgress(update ProgressUpdate) {
	select {
	case e.progressChan <- update:
	default:
		// Channel is full, skip this update
	}
}

// GetAppDataPaths returns the discovered app data paths
func (e *Engine) GetAppDataPaths() map[string]string {
	return e.appDataPaths
}

// GetBackupDirectory returns the backup directory path
func (e *Engine) GetBackupDirectory() string {
	return e.backupBaseDir
}

// GenerateCacheCleaningReport 生成缓存重置报告
func (e *Engine) GenerateCacheCleaningReport(appName string, stats map[string]*CacheStats) string {
	var report strings.Builder

	report.WriteString(e.localizeMessage("CacheReportTitle", map[string]interface{}{
		"AppName": appName,
	}) + "\n")

	var totalDirs, totalCleanedDirs int
	var totalSize int64

	for dirName, stat := range stats {
		if stat.DirCount > 0 {
			report.WriteString(e.localizeMessage("CacheReportItem", map[string]interface{}{
				"DirName": dirName,
				"Cleaned": stat.CleanedDirs,
				"Total":   stat.DirCount,
				"Size":    e.FormatSize(stat.TotalSize),
			}) + "\n")

			totalDirs += stat.DirCount
			totalCleanedDirs += stat.CleanedDirs
			totalSize += stat.TotalSize
		}
	}

	report.WriteString(e.localizeMessage("CacheReportTotal", map[string]interface{}{
		"Cleaned": totalCleanedDirs,
		"Total":   totalDirs,
		"Size":    e.FormatSize(totalSize),
	}) + "\n")

	return report.String()
}

// DiscoverCacheInfo 发现并报告应用程序缓存信息
func (e *Engine) DiscoverCacheInfo(appPath, appName string) map[string]int64 {
	cacheDirs := e.config.CleaningOptions.CacheDirectories
	cacheInfo := make(map[string]int64)

	for _, dirName := range cacheDirs {
		foundDirs := e.findDirectoriesRecursive(appPath, []string{dirName})

		var totalSize int64
		for _, dir := range foundDirs {
			size := e.GetDirectorySize(dir)
			totalSize += size
		}

		if len(foundDirs) > 0 {
			cacheInfo[dirName] = totalSize
			log.Info().Str("dirName", dirName).Int("count", len(foundDirs)).Str("size", e.FormatSize(totalSize)).Msg("Cache info")
		}
	}

	return cacheInfo
}

// Helper functions
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func copyDirectory(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		return copyFile(path, destPath)
	})
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestSQLiteConnection 测试SQLite连接和操作，用于调试
func (e *Engine) TestSQLiteConnection(dbPath string) error {

	// 检查文件是否存在
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return fmt.Errorf("database file does not exist: %s", dbPath)
	}

	// 尝试不同的连接参数
	connectionStrings := []string{
		dbPath,
		dbPath + "?_journal=WAL",
		dbPath + "?mode=ro", // 只读模式
		dbPath + "?_timeout=5000",
	}

	for _, connStr := range connectionStrings {
		log.Debug().Str("connection", connStr).Msg("Trying connection string")

		db, err := sql.Open("sqlite", connStr)
		if err != nil {
			log.Error().Str("connection", connStr).Err(err).Msg("Failed to open database")
			continue
		}
		defer db.Close()

		// 测试连接
		if err := db.Ping(); err != nil {
			log.Error().Str("connection", connStr).Err(err).Msg("Failed to ping database")
			continue
		}

		log.Info().Str("connection", connStr).Msg("Successfully connected to database")

		// 列出所有表
		rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table'")
		if err != nil {
			log.Error().Err(err).Msg("Failed to list tables")
			continue
		}

		var tables []string
		for rows.Next() {
			var tableName string
			if err := rows.Scan(&tableName); err != nil {
				log.Error().Err(err).Msg("Failed to scan table name")
				continue
			}
			tables = append(tables, tableName)
		}
		rows.Close()

		log.Info().Strs("tables", tables).Int("count", len(tables)).Msg("Database tables")

		// 尝试读取ItemTable表的内容（如果存在）
		if contains(tables, "ItemTable") {
			log.Info().Msg("Found ItemTable, trying to read contents")

			rows, err := db.Query("SELECT key, value FROM ItemTable LIMIT 10")
			if err != nil {
				log.Error().Err(err).Msg("Failed to query ItemTable")
				continue
			}

			var items []string
			for rows.Next() {
				var key, value string
				if err := rows.Scan(&key, &value); err != nil {
					log.Error().Err(err).Msg("Failed to scan row")
					continue
				}
				items = append(items, fmt.Sprintf("%s=%s", key, value))
			}
			rows.Close()

			log.Info().Strs("items", items).Int("count", len(items)).Msg("ItemTable contents (sample)")
			return nil // 成功找到并读取了ItemTable
		}
	}

	return fmt.Errorf("could not successfully connect and read from database")
}

// contains 检查字符串切片是否包含指定字符串
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
