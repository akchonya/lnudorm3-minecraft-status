package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	MAX_RETRIES      = 3
	RETRY_DELAY      = 3 * time.Second
	CHECK_INTERVAL   = 30 * time.Second
	CLEANUP_INTERVAL = 24 * time.Hour
	ONE_DAY_IN_MS    = 24 * 60 * 60 * 1000
	JSON_FILE        = "status.json"
	TIMEOUT          = 3 * time.Second
)

type ServerStatus struct {
	Online      bool
	PlayerCount int
	Players     []string
}

type StatusEntry struct {
	ID          int64    `json:"id"`
	Online      bool     `json:"online"`
	LastChecked int64    `json:"lastChecked"`
	Players     []string `json:"players"`
}

type StatusStore struct {
	Entries []StatusEntry `json:"entries"`
	mu      sync.RWMutex
}

type Config struct {
	ServerHost     string
	ServerPort     uint16
	TelegramToken  string
	TelegramChatID string
}

var (
	store  *StatusStore
	config Config
)

func init() {
	config = Config{
		ServerHost:     getEnv("SERVER_HOST", ""),
		ServerPort:     uint16(getEnvInt("SERVER_PORT", 25565)),
		TelegramToken:  getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID: getEnv("TELEGRAM_CHAT_ID", ""),
	}

	if config.ServerHost == "" {
		log.Fatal("SERVER_HOST environment variable is required")
	}
	if config.TelegramToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable is required")
	}
	if config.TelegramChatID == "" {
		log.Fatal("TELEGRAM_CHAT_ID environment variable is required")
	}

	store = &StatusStore{Entries: []StatusEntry{}}
	loadStore()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var result int
		if _, err := fmt.Sscanf(value, "%d", &result); err == nil {
			return result
		}
	}
	return defaultValue
}

func loadStore() {
	store.mu.Lock()
	defer store.mu.Unlock()

	data, err := ioutil.ReadFile(JSON_FILE)
	if err != nil {
		if os.IsNotExist(err) {
			store.Entries = []StatusEntry{}
			return
		}
		log.Printf("Error reading status file: %v", err)
		return
	}

	if err := json.Unmarshal(data, store); err != nil {
		log.Printf("Error parsing status file: %v", err)
		store.Entries = []StatusEntry{}
	}
}

func saveStore() {
	store.mu.Lock()
	defer store.mu.Unlock()

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		log.Printf("Error marshaling status: %v", err)
		return
	}

	if err := ioutil.WriteFile(JSON_FILE, data, 0644); err != nil {
		log.Printf("Error writing status file: %v", err)
	}
}

func getLatest() *StatusEntry {
	store.mu.RLock()
	defer store.mu.RUnlock()

	if len(store.Entries) == 0 {
		return nil
	}

	latest := store.Entries[0]
	for _, entry := range store.Entries {
		if entry.LastChecked > latest.LastChecked {
			latest = entry
		}
	}
	return &latest
}

func insertStatus(online bool, lastChecked int64, players []string) {
	store.mu.Lock()
	defer store.mu.Unlock()

	newID := time.Now().UnixNano()
	entry := StatusEntry{
		ID:          newID,
		Online:      online,
		LastChecked: lastChecked,
		Players:     players,
	}

	store.Entries = append(store.Entries, entry)
}

func cleanupOld() {
	store.mu.Lock()
	defer store.mu.Unlock()

	cutoff := time.Now().Unix()*1000 - ONE_DAY_IN_MS
	filtered := []StatusEntry{}

	for _, entry := range store.Entries {
		if entry.LastChecked >= cutoff {
			filtered = append(filtered, entry)
		}
	}

	store.Entries = filtered
}

func escapeHtml(s string) string {
	result := s
	replacements := map[string]string{
		"&":  "&amp;",
		"<":  "&lt;",
		">":  "&gt;",
		"\"": "&quot;",
		"'":  "&#39;",
	}
	for old, new := range replacements {
		result = replaceAll(result, old, new)
	}
	return result
}

func replaceAll(s, old, new string) string {
	return strings.ReplaceAll(s, old, new)
}

func httpPost(url string, data []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	return client.Do(req)
}

func bold(s string) string {
	return fmt.Sprintf("<b>%s</b>", escapeHtml(s))
}

func sendTelegramMessage(text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", config.TelegramToken)

	payload := map[string]interface{}{
		"chat_id":    config.TelegramChatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := httpPost(url, jsonData)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s - %s", resp.Status, string(body))
	}

	return nil
}

func updateChatTitle(title string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/setChatTitle", config.TelegramToken)

	payload := map[string]interface{}{
		"chat_id": config.TelegramChatID,
		"title":   title,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := httpPost(url, jsonData)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s - %s", resp.Status, string(body))
	}

	return nil
}

func pingMinecraftServer(host string, port uint16) (*ServerStatus, error) {
	address := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", address, TIMEOUT)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(TIMEOUT))

	hostBytes := []byte(host)
	packet := new(bytes.Buffer)

	writeVarInt(packet, 0)
	writeVarInt(packet, 47)
	writeVarInt(packet, int32(len(hostBytes)))
	packet.Write(hostBytes)
	binary.Write(packet, binary.BigEndian, uint16(port))
	writeVarInt(packet, 1)

	packetData := packet.Bytes()
	packetLen := new(bytes.Buffer)
	writeVarInt(packetLen, int32(len(packetData)))

	_, err = conn.Write(append(packetLen.Bytes(), packetData...))
	if err != nil {
		return nil, err
	}

	statusReq := new(bytes.Buffer)
	writeVarInt(statusReq, 0)
	statusReqData := statusReq.Bytes()
	statusReqLen := new(bytes.Buffer)
	writeVarInt(statusReqLen, int32(len(statusReqData)))
	_, err = conn.Write(append(statusReqLen.Bytes(), statusReqData...))
	if err != nil {
		return nil, err
	}

	responseLen, err := readVarInt(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to read response length: %v", err)
	}

	if responseLen <= 0 || responseLen > 65535 {
		return nil, fmt.Errorf("invalid response length: %d", responseLen)
	}

	responseData := make([]byte, responseLen)
	totalRead := 0
	for totalRead < int(responseLen) {
		n, err := conn.Read(responseData[totalRead:])
		if err != nil {
			return nil, fmt.Errorf("failed to read response data: %v", err)
		}
		totalRead += n
	}

	responseBuf := bytes.NewBuffer(responseData)

	_, err = readVarInt(responseBuf)
	if err != nil {
		return nil, err
	}

	jsonLen, err := readVarInt(responseBuf)
	if err != nil {
		return nil, err
	}

	jsonData := make([]byte, jsonLen)
	_, err = responseBuf.Read(jsonData)
	if err != nil {
		return nil, err
	}

	var statusJSON map[string]interface{}
	if err := json.Unmarshal(jsonData, &statusJSON); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %v", err)
	}

	version, ok := statusJSON["version"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid server response: missing version field")
	}
	versionName, ok := version["name"].(string)
	if !ok || versionName == "" {
		return nil, fmt.Errorf("invalid server response: missing or empty version name")
	}

	status := &ServerStatus{Online: true}

	if players, ok := statusJSON["players"].(map[string]interface{}); ok {
		if online, ok := players["online"].(float64); ok {
			status.PlayerCount = int(online)
		}

		if sample, ok := players["sample"].([]interface{}); ok {
			playerList := []string{}
			for _, p := range sample {
				if player, ok := p.(map[string]interface{}); ok {
					if name, ok := player["name"].(string); ok {
						playerList = append(playerList, name)
					}
				}
			}
			status.Players = playerList
		}
	}

	return status, nil
}

func writeVarInt(buf *bytes.Buffer, value int32) {
	for {
		if (value & ^0x7F) == 0 {
			buf.WriteByte(byte(value))
			return
		}
		buf.WriteByte(byte((value & 0x7F) | 0x80))
		value = int32(uint32(value) >> 7)
	}
}

func readVarInt(reader interface{}) (int32, error) {
	var b byte
	var result int32
	var shift uint

	for {
		var err error
		switch r := reader.(type) {
		case *bytes.Buffer:
			b, err = r.ReadByte()
		case net.Conn:
			var data [1]byte
			_, err = r.Read(data[:])
			b = data[0]
		default:
			return 0, fmt.Errorf("unsupported reader type")
		}

		if err != nil {
			return 0, err
		}

		result |= int32(b&0x7F) << shift
		if (b & 0x80) == 0 {
			break
		}
		shift += 7
		if shift >= 32 {
			return 0, fmt.Errorf("varint too long")
		}
	}

	return result, nil
}

func checkServer() {
	latest := getLatest()

	var online bool
	var statusResponse *ServerStatus
	var err error

	for attempt := 1; attempt <= MAX_RETRIES; attempt++ {
		statusResponse, err = pingMinecraftServer(config.ServerHost, config.ServerPort)
		if err == nil && statusResponse != nil {
			break
		}

		if attempt < MAX_RETRIES {
			log.Printf("Server check attempt %d failed, retrying...", attempt)
			time.Sleep(RETRY_DELAY)
		} else {
			log.Printf("Server check failed after %d attempts: %v", MAX_RETRIES, err)
		}
	}

	previousPlayers := []string{}
	if latest != nil {
		previousPlayers = latest.Players
	}

	currentPlayers := previousPlayers
	playerDataReliable := false
	playerCount := 0

	if statusResponse != nil {

		// ⭐⭐⭐ MODIFICATION HERE ⭐⭐⭐
		online = statusResponse.PlayerCount > 0
		// ⭐⭐⭐ END MODIFICATION ⭐⭐⭐

		playerCount = statusResponse.PlayerCount

		if len(statusResponse.Players) > 0 {
			currentPlayers = []string{}
			playerSet := make(map[string]bool)
			for _, playerName := range statusResponse.Players {
				if playerName != "" {
					if !playerSet[playerName] {
						currentPlayers = append(currentPlayers, playerName)
						playerSet[playerName] = true
					}
				}
			}
			playerDataReliable = len(currentPlayers) > 0
		}

		if !playerDataReliable {
			if playerCount == 0 {
				currentPlayers = []string{}
				playerDataReliable = true
			} else if playerCount >= 0 {
				if len(previousPlayers) > playerCount {
					currentPlayers = previousPlayers[:playerCount]
				} else {
					currentPlayers = previousPlayers
				}
			}
		}
	} else {
		currentPlayers = []string{}
		if len(previousPlayers) > 0 {
			playerDataReliable = true
		}
		online = false
	}

	currentPlayerSet := make(map[string]bool)
	for _, p := range currentPlayers {
		currentPlayerSet[p] = true
	}

	previousPlayerSet := make(map[string]bool)
	for _, p := range previousPlayers {
		previousPlayerSet[p] = true
	}

	var joinedPlayers []string
	var leftPlayers []string

	if playerDataReliable {
		for _, p := range currentPlayers {
			if !previousPlayerSet[p] {
				joinedPlayers = append(joinedPlayers, p)
			}
		}
		for _, p := range previousPlayers {
			if !currentPlayerSet[p] {
				leftPlayers = append(leftPlayers, p)
			}
		}
	}

	insertStatus(online, time.Now().Unix()*1000, currentPlayers)
	saveStore()

	if playerDataReliable && (len(joinedPlayers) > 0 || len(leftPlayers) > 0) {
		var changes []string

		if len(joinedPlayers) > 0 {
			if len(joinedPlayers) == 1 {
				changes = append(changes, fmt.Sprintf("😎 %s зайшов на сервер", bold(joinedPlayers[0])))
			} else {
				joinedBold := make([]string, len(joinedPlayers))
				for i, p := range joinedPlayers {
					joinedBold[i] = bold(p)
				}
				changes = append(changes, fmt.Sprintf("😎 на сервер зайшли: %s", joinStrings(joinedBold, ", ")))
			}
		}

		if len(leftPlayers) > 0 {
			if len(leftPlayers) == 1 {
				changes = append(changes, fmt.Sprintf("🥺 %s вийшов", bold(leftPlayers[0])))
			} else {
				leftBold := make([]string, len(leftPlayers))
				for i, p := range leftPlayers {
					leftBold[i] = bold(p)
				}
				changes = append(changes, fmt.Sprintf("🥺 вийшли: %s", joinStrings(leftBold, ", ")))
			}
		}

		if len(changes) > 0 {
			message := joinStrings(changes, "\n")
			if err := sendTelegramMessage(message); err != nil {
				log.Printf("Error sending Telegram message: %v", err)
			}
		}
	}

	chatTitle := "🔴 lnudorm3 minecraft йоу"
	if online {
		chatTitle = "🟢 lnudorm3 minecraft йоу"
	}

	if err := updateChatTitle(chatTitle); err != nil {
		log.Printf("Error updating chat title: %v", err)
	}

	log.Printf("Server status: %s", map[bool]string{true: "online", false: "offline"}[online])
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

func main() {
	log.Println("Starting Minecraft server status checker...")

	checkServer()

	ticker := time.NewTicker(CHECK_INTERVAL)
	defer ticker.Stop()

	cleanupTicker := time.NewTicker(CLEANUP_INTERVAL)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ticker.C:
			checkServer()
		case <-cleanupTicker.C:
			log.Println("Cleaning up old status entries...")
			cleanupOld()
			saveStore()
		}
	}
}
