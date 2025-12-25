package main

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
)

// DanmuItem 一条解析后的弹幕
type DanmuItem struct {
	StartTime float64 // 相对开播的秒数（如 123.456）
	Username  string
	UID       int64
	Content   string
}

// extractLiveID 从 ASS 文件头提取 LiveID
func extractLiveID(assFile string) (string, error) {
	file, err := os.Open(assFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inScript := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "[Script Info]" {
			inScript = true
			continue
		}
		if inScript && strings.HasPrefix(line, "[") {
			break
		}
		if inScript && strings.HasPrefix(line, "; LiveID:") {
			re := regexp.MustCompile(`;\s*LiveID:\s*(\S+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				return matches[1], nil
			}
		}
	}
	return "", nil // 未找到
}

// timeToSeconds 将 "H:MM:SS.cc" 或 "MM:SS.cc" 转为秒数
func timeToSeconds(timeStr string) (float64, error) {
	parts := strings.Split(timeStr, ":")
	var h, m, s string
	switch len(parts) {
	case 2:
		h, m, s = "0", parts[0], parts[1]
	case 3:
		h, m, s = parts[0], parts[1], parts[2]
	default:
		return 0, fmt.Errorf("invalid time format: %s", timeStr)
	}

	hour, _ := strconv.Atoi(h)
	min, _ := strconv.Atoi(m)
	sec, _ := strconv.ParseFloat(s, 64)

	return float64(hour*3600+min*60) + sec, nil
}

// extractPlainText 去除 ASS 格式标签 {\...}
func extractPlainText(raw string) string {
	re := regexp.MustCompile(`\{\\[^}]*}`)
	return strings.TrimSpace(re.ReplaceAllString(raw, ""))
}

// parseUserInfo 解析 "用户名 (123456)" → name, uid
func parseUserInfo(rawUser string) (string, int64) {
	re := regexp.MustCompile(`^(.+?)\s*\((\d+)\)$`)
	matches := re.FindStringSubmatch(strings.TrimSpace(rawUser))
	if len(matches) == 3 {
		uid, _ := strconv.ParseInt(matches[2], 10, 64)
		return strings.TrimSpace(matches[1]), uid
	}
	return strings.TrimSpace(rawUser), 0
}

// parseASSEvents 解析 [Events] 中的弹幕
func parseASSEvents(assFile string) ([]DanmuItem, error) {
	file, err := os.Open(assFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var danmakuList []DanmuItem
	inEvents := false
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "[Events]" {
			inEvents = true
			continue
		}
		if !inEvents {
			continue
		}
		if strings.HasPrefix(line, "[") {
			break
		}
		if strings.HasPrefix(line, "Dialogue:") {
			parts := strings.SplitN(line, ",", 10)
			if len(parts) < 10 {
				continue
			}
			startTimeStr := parts[1]
			rawUser := parts[4]
			rawText := parts[9]

			startSec, err := timeToSeconds(startTimeStr)
			if err != nil {
				lPrintErrf("跳过无效时间格式: %s", startTimeStr)
				continue
			}

			username, uid := parseUserInfo(rawUser)
			content := extractPlainText(rawText)

			danmakuList = append(danmakuList, DanmuItem{
				StartTime: startSec,
				Username:  username,
				UID:       uid,
				Content:   content,
			})
		}
	}
	return danmakuList, nil
}

// getLiveStartTime 获取直播开始时间（返回 time.Time）
func getLiveStartTime(db *sql.DB, dbType, liveID string) (time.Time, error) {
	switch dbType {
	case "mysql":
		var startTime time.Time
		err := db.QueryRow("SELECT startTime FROM live WHERE liveId = ?", liveID).Scan(&startTime)
		return startTime, err

	case "sqlite":
		var timestampMs int64
		err := db.QueryRow("SELECT startTime FROM acfunlive WHERE liveId = ?", liveID).Scan(&timestampMs)
		if err != nil {
			return time.Time{}, err
		}
		// 毫秒转 time.Time（假设是 UTC 时间戳，转为本地时区）
		t := time.Unix(0, timestampMs*int64(time.Millisecond)).UTC()
		beijing, _ := time.LoadLocation("Asia/Shanghai")
		return t.In(beijing), nil

	default:
		return time.Time{}, fmt.Errorf("unsupported db type: %s", dbType)
	}
}

func insertDanmuToMySQL(db *sql.DB, liveID string, danmakuList []DanmuItem, baseTime time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. 处理用户（去重）
	userMap := make(map[int64]string)
	for _, d := range danmakuList {
		if d.UID != 0 {
			userMap[d.UID] = d.Username
		}
	}

	now := time.Now()
	for uid, name := range userMap {
		var existingName, fNames sql.NullString
		err := tx.QueryRow("SELECT name, fNames FROM user WHERE uid = ?", uid).Scan(&existingName, &fNames)
		if errors.Is(err, sql.ErrNoRows) {
			// 插入新用户
			_, err = tx.Exec("INSERT INTO user (uid, name, fNames) VALUES (?, ?, ?)", uid, name, nil)
			if err != nil {
				lPrintErrf("插入用户失败 UID=%d: %v", uid, err)
				continue
			}
			_, _ = tx.Exec("INSERT INTO userOld (uid, oldName, startDate, endDate) VALUES (?, ?, ?, ?)", uid, name, now, nil)
			lPrintf("插入新用户 UID: %d, 名称: %s", uid, name)
		} else if err == nil {
			if existingName.String != name {
				// 更新名称
				newFNames := existingName.String
				if fNames.Valid && fNames.String != "" {
					newFNames = existingName.String + "," + fNames.String
				}
				_, _ = tx.Exec("UPDATE user SET name = ?, fNames = ? WHERE uid = ?", name, newFNames, uid)
				_, _ = tx.Exec("UPDATE userOld SET endDate = ? WHERE uid = ? AND oldName = ? AND endDate IS NULL", now, uid, existingName.String)
				_, _ = tx.Exec("INSERT INTO userOld (uid, oldName, startDate, endDate) VALUES (?, ?, ?, ?)", uid, name, now, nil)
				lPrintf("更新用户 UID: %d, 新名称: %s", uid, name)
			}
		}
	}

	// 2. 清除旧弹幕
	_, _ = tx.Exec("DELETE FROM danmaku WHERE liveId = ?", liveID)

	// 3. 插入新弹幕
	stmt, err := tx.Prepare("INSERT INTO danmaku (liveId, startTime, sendTime, uid, content) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	count := 0
	for _, d := range danmakuList {
		if d.UID == 0 {
			continue
		}
		sendTime := baseTime.Add(time.Duration(d.StartTime * float64(time.Second)))
		_, err := stmt.Exec(liveID, d.StartTime, sendTime, d.UID, d.Content)
		if err != nil {
			lPrintErrf("插入弹幕失败: %v", err)
			continue
		}
		count++
	}

	err = tx.Commit()
	if err != nil {
		return err
	}
	lPrintf("成功写入 %d 条弹幕到 MySQL", count)
	return nil
}

func insertDanmuToSQLite(db *sql.DB, liveID string, danmakuList []DanmuItem, baseTime time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 确保表存在
	_, _ = tx.Exec(`CREATE TABLE IF NOT EXISTS streamer (uid INTEGER NOT NULL PRIMARY KEY, name TEXT NOT NULL)`)
	_, _ = tx.Exec(`CREATE TABLE IF NOT EXISTS danmaku (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		liveId TEXT NOT NULL,
		startTime REAL NOT NULL,
		sendTime TEXT NOT NULL,
		uid INTEGER NOT NULL,
		content TEXT NOT NULL
	)`)

	// 处理 streamer
	userMap := make(map[int64]string)
	for _, d := range danmakuList {
		if d.UID != 0 {
			userMap[d.UID] = d.Username
		}
	}

	for uid, name := range userMap {
		var existingName string
		err := tx.QueryRow("SELECT name FROM streamer WHERE uid = ?", uid).Scan(&existingName)
		if errors.Is(err, sql.ErrNoRows) {
			_, _ = tx.Exec("INSERT INTO streamer (uid, name) VALUES (?, ?)", uid, name)
			lPrintf("插入新 streamer UID: %d, 名称: %s", uid, name)
		} else if err == nil && existingName != name {
			_, _ = tx.Exec("UPDATE streamer SET name = ? WHERE uid = ?", name, uid)
			lPrintf("更新 streamer UID: %d, 新名称: %s", uid, name)
		}
	}

	// 清除旧弹幕
	_, _ = tx.Exec("DELETE FROM danmaku WHERE liveId = ?", liveID)

	// 插入新弹幕
	stmt, err := tx.Prepare("INSERT INTO danmaku (liveId, startTime, sendTime, uid, content) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	count := 0
	for _, d := range danmakuList {
		if d.UID == 0 {
			continue
		}
		sendTime := baseTime.Add(time.Duration(d.StartTime * float64(time.Second)))
		sendTimeStr := sendTime.UTC().Format("2006-01-02 15:04:05")
		_, err := stmt.Exec(liveID, d.StartTime, sendTimeStr, d.UID, d.Content)
		if err != nil {
			lPrintErrf("插入弹幕失败: %v", err)
			continue
		}
		count++
	}

	err = tx.Commit()
	if err != nil {
		return err
	}
	lPrintf("成功写入 %d 条弹幕到 SQLite", count)
	return nil
}

// extractLiveStartTimeFromASS 从 ASS 文件注释中提取 LiveStartTime
// 支持格式: ; LiveStartTime: 2025-12-25 10:00:00.123
// 返回 time.Time（北京时间），如果未找到或解析失败，返回零值和 error
func extractLiveStartTimeFromASS(assFile string) (time.Time, error) {
	file, err := os.Open(assFile)
	if err != nil {
		return time.Time{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for i := 0; i < 50 && scanner.Scan(); i++ {
		line := scanner.Text()
		if strings.HasPrefix(line, "; LiveStartTime:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) < 2 {
				continue
			}
			timeStr := strings.TrimSpace(parts[1])

			for _, layout := range []string{
				"2006-01-02 15:04:05.000",
				"2006-01-02 15:04:05",
			} {
				if t, err := time.Parse(layout, timeStr); err == nil {
					// t 是 UTC 时间（因为 time.Parse 默认按 UTC 解析无时区字符串）

					// 转换为北京时间（CST / Asia/Shanghai），与 getLiveStartTime(SQLite) 保持一致
					cst, _ := time.LoadLocation("Asia/Shanghai")
					return t.In(cst), nil
				}
			}
			return time.Time{}, fmt.Errorf("无法解析 LiveStartTime: %s", timeStr)
		}
	}
	return time.Time{}, fmt.Errorf("未找到 LiveStartTime 注释")
}

// SaveDanmuToDB 将 ASS 弹幕文件写入数据库
func SaveDanmuToDB(assFile string) {
	if config.Database.Type == "" {
		return // 未启用
	}

	liveID, err := extractLiveID(assFile)
	if err != nil || liveID == "" {
		lPrintErrf("无法提取 LiveID: %v", err)
		return
	}

	danmakuList, err := parseASSEvents(assFile)
	if err != nil || len(danmakuList) == 0 {
		lPrintWarn("无有效弹幕数据")
		return
	}

	// 打开数据库
	var db *sql.DB
	var dsn string
	switch config.Database.Type {
	case "sqlite":
		dsn = config.Database.SqliteFile
		db, err = sql.Open("sqlite3", dsn)
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			config.Database.User,
			config.Database.Password,
			config.Database.Host,
			config.Database.Port,
			config.Database.Database)
		db, err = sql.Open("mysql", dsn)
	default:
		lPrintErrf("不支持的数据库类型: %s", config.Database.Type)
		return
	}
	if err != nil {
		lPrintErrf("数据库连接失败: %v", err)
		return
	}
	defer db.Close()

	// 获取开播时间
	baseTime, err := getLiveStartTime(db, config.Database.Type, liveID)
	if err != nil {
		lPrintWarnf("数据库未找到直播记录 liveId=%s，尝试从 ASS 注释读取...", liveID)

		// 2. 从 ASS 文件读取（读取的baseTime是东八区时间）
		baseTime, err = extractLiveStartTimeFromASS(assFile)
		if err != nil {
			lPrintErrf("无法从 ASS 获取 LiveStartTime: %v", err)
			return
		}
		lPrintf("成功从 ASS 注释读取开播时间: %v", baseTime.Format("2006-01-02 15:04:05.000"))
	}

	// 写入
	switch config.Database.Type {
	case "mysql":
		err = insertDanmuToMySQL(db, liveID, danmakuList, baseTime)
	case "sqlite":
		// 东八区时间传进去，格式化前需要调用.UTC()
		err = insertDanmuToSQLite(db, liveID, danmakuList, baseTime)
	}
	if err != nil {
		lPrintErrf("弹幕入库失败: %v", err)
	}
}
