package transcript

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Request struct {
	ID          string
	Agent       string
	Model       string
	Message     string
	RepoRoot    string
	SessionID   string
	TurnID      string
	Timestamp   string
	EditedFiles []string
	SourcePath  string
}

type Cache struct {
	mu    sync.Mutex
	files map[string]cachedFile
}

type cachedFile struct {
	mtime    int64
	size     int64
	requests []Request
}

func NewCache() *Cache {
	return &Cache{files: map[string]cachedFile{}}
}

func RequestsByRepo(repoRoot string) ([]Request, error) {
	return RequestsByRepoContext(context.Background(), repoRoot, nil)
}

func RequestsByRepoContext(ctx context.Context, repoRoot string, cache *Cache) ([]Request, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	var all []Request
	var errs []error
	seen := map[string]bool{}
	for _, scan := range []func(context.Context, string, *Cache, map[string]bool) ([]Request, error){
		scanClaude,
		scanCodex,
		scanGemini,
	} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		requests, err := scan(ctx, root, cache, seen)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		all = append(all, requests...)
	}
	if cache != nil {
		cache.prune(seen)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Timestamp != all[j].Timestamp {
			return all[i].Timestamp > all[j].Timestamp
		}
		return all[i].ID > all[j].ID
	})
	if len(errs) > 0 && len(all) == 0 {
		return nil, errors.Join(errs...)
	}
	return all, nil
}

func scanClaude(ctx context.Context, repoRoot string, cache *Cache, seen map[string]bool) ([]Request, error) {
	home, err := claudeHomeDir()
	if err != nil {
		return nil, nil
	}
	dir := filepath.Join(home, "projects", escapeClaudeProjectPath(repoRoot))
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	return scanJSONL(ctx, paths, repoRoot, "claude-jsonl", parseClaudeRecord, cache, seen)
}

func scanCodex(ctx context.Context, repoRoot string, cache *Cache, seen map[string]bool) ([]Request, error) {
	home, err := codexHomeDir()
	if err != nil {
		return nil, nil
	}
	root := filepath.Join(home, "sessions")
	paths, err := jsonFiles(root, ".jsonl")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	paths = filterCodexSessionPaths(ctx, paths, repoRoot, cache)
	return scanJSONL(ctx, paths, repoRoot, "codex-jsonl", parseCodexRecord, cache, seen)
}

func scanGemini(ctx context.Context, repoRoot string, cache *Cache, seen map[string]bool) ([]Request, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	geminiHome := filepath.Join(home, ".gemini")
	var requests []Request
	projectSlug := geminiProjectSlug(geminiHome, repoRoot)
	if projectSlug != "" {
		chatRoot := filepath.Join(geminiHome, "tmp", projectSlug, "chats")
		paths, err := jsonFiles(chatRoot, ".json")
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, path := range paths {
			got, err := scanCachedPath(ctx, path, repoRoot, "gemini-chat", cache, seen, func() ([]Request, error) {
				return scanGeminiChat(path, repoRoot)
			})
			if err != nil {
				return nil, err
			}
			requests = append(requests, got...)
		}
	}
	paths, err := jsonFiles(geminiHome, ".jsonl")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	paths = filterGeminiJSONLPaths(paths, geminiHome, projectSlug)
	got, err := scanJSONL(ctx, paths, repoRoot, "gemini-jsonl", parseGeminiJSONLRecord, cache, seen)
	if err != nil {
		return nil, err
	}
	requests = append(requests, got...)
	return requests, nil
}

type parseState struct {
	agent       string
	repoRoot    string
	sessionID   string
	turnID      string
	model       string
	cwd         string
	repoMatched bool
	current     *Request
	requests    []Request
}

type recordParser func(map[string]any, *parseState, string)

func scanJSONL(ctx context.Context, paths []string, repoRoot, kind string, parser recordParser, cache *Cache, seen map[string]bool) ([]Request, error) {
	sort.Strings(paths)
	var all []Request
	for _, path := range paths {
		requests, err := scanCachedPath(ctx, path, repoRoot, kind, cache, seen, func() ([]Request, error) {
			return scanJSONLFile(ctx, path, repoRoot, parser)
		})
		if err != nil {
			return nil, err
		}
		all = append(all, requests...)
	}
	return all, nil
}

func scanJSONLFile(ctx context.Context, path, repoRoot string, parser recordParser) ([]Request, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	state := parseState{repoRoot: repoRoot}
	reader := bufio.NewReader(file)
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			_ = file.Close()
			return nil, readErr
		}
		line = strings.TrimSpace(line)
		if line == "" && errors.Is(readErr, io.EOF) {
			break
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err == nil {
			parser(record, &state, path)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	closeErr := file.Close()
	if closeErr != nil {
		return nil, closeErr
	}
	for i := range state.requests {
		finalizeRequest(&state.requests[i])
	}
	return state.requests, nil
}

func scanCachedPath(ctx context.Context, path, repoRoot, kind string, cache *Cache, seen map[string]bool, parse func() ([]Request, error)) ([]Request, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := kind + "\x00" + path
	if seen != nil {
		seen[key] = true
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	mtime := info.ModTime().UnixNano()
	size := info.Size()
	if cache != nil {
		if requests, ok := cache.get(key, mtime, size); ok {
			return requests, nil
		}
	}
	requests, err := parse()
	if err != nil {
		return nil, err
	}
	for i := range requests {
		requests[i].RepoRoot = repoRoot
	}
	if cache != nil {
		cache.set(key, mtime, size, requests)
	}
	return requests, nil
}

func (c *Cache) get(key string, mtime, size int64) ([]Request, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.files == nil {
		c.files = map[string]cachedFile{}
	}
	item, ok := c.files[key]
	if !ok || item.mtime != mtime || item.size != size {
		return nil, false
	}
	return cloneRequests(item.requests), true
}

func (c *Cache) set(key string, mtime, size int64, requests []Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.files == nil {
		c.files = map[string]cachedFile{}
	}
	c.files[key] = cachedFile{mtime: mtime, size: size, requests: cloneRequests(requests)}
}

func (c *Cache) prune(seen map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.files {
		if !seen[key] {
			delete(c.files, key)
		}
	}
}

func cloneRequests(requests []Request) []Request {
	if len(requests) == 0 {
		return nil
	}
	cloned := make([]Request, len(requests))
	copy(cloned, requests)
	for i := range cloned {
		cloned[i].EditedFiles = append([]string(nil), cloned[i].EditedFiles...)
	}
	return cloned
}

func parseClaudeRecord(record map[string]any, state *parseState, source string) {
	state.agent = "claude"
	if sessionID := stringValue(record, "sessionId"); sessionID != "" {
		state.sessionID = sessionID
	}
	if cwd := cleanAbsPath(stringValue(record, "cwd")); cwd != "" {
		state.cwd = cwd
		state.repoMatched = cwd == state.repoRoot
	}
	message := objectValue(record, "message")
	if model := stringValue(message, "model"); model != "" {
		state.model = model
		if state.current != nil && state.current.Model == "" {
			state.current.Model = model
		}
	}
	if record["type"] == "user" && state.repoMatched {
		text := claudeUserText(message["content"])
		if text != "" {
			state.addRequest(Request{
				Agent:      "claude",
				Model:      state.model,
				Message:    text,
				RepoRoot:   state.repoRoot,
				SessionID:  state.sessionID,
				TurnID:     stringValue(record, "uuid"),
				Timestamp:  timestampOf(record, nil),
				SourcePath: source,
			})
		}
	}
	for _, file := range claudeEditedFiles(message["content"]) {
		state.addEditedFile(file)
	}
}

func parseCodexRecord(record map[string]any, state *parseState, source string) {
	state.agent = "codex"
	payload := objectValue(record, "payload")
	switch record["type"] {
	case "session_meta":
		if sessionID := stringValue(payload, "id"); sessionID != "" {
			state.sessionID = sessionID
		}
		updateCodexContext(payload, state)
	case "turn_context":
		updateCodexContext(payload, state)
	case "event_msg":
		if payload["type"] == "user_message" && state.repoMatched {
			message := strings.TrimSpace(stringValue(payload, "message"))
			if message != "" {
				state.addRequest(Request{
					Agent:      "codex",
					Model:      state.model,
					Message:    message,
					RepoRoot:   state.repoRoot,
					SessionID:  state.sessionID,
					TurnID:     state.turnID,
					Timestamp:  timestampOf(record, payload),
					SourcePath: source,
				})
			}
		}
	case "response_item":
		if model := stringValue(payload, "model"); model != "" {
			state.model = model
			if state.current != nil && state.current.Model == "" {
				state.current.Model = model
			}
		}
		for _, file := range codexEditedFiles(payload) {
			state.addEditedFile(file)
		}
	}
}

func parseGeminiJSONLRecord(record map[string]any, state *parseState, source string) {
	state.agent = "gemini"
	if cwd := cleanAbsPath(firstString(record, "cwd", "workingDirectory", "projectRoot")); cwd != "" {
		state.cwd = cwd
		state.repoMatched = cwd == state.repoRoot
	}
	if sessionID := firstString(record, "sessionId", "session_id"); sessionID != "" {
		state.sessionID = sessionID
	}
	if model := stringValue(record, "model"); model != "" {
		state.model = model
	}
	typ := strings.ToLower(firstString(record, "type", "role"))
	if typ == "user" && state.repoMatched {
		message := geminiContentText(record["content"])
		if message != "" {
			state.addRequest(Request{
				Agent:      "gemini",
				Model:      state.model,
				Message:    message,
				RepoRoot:   state.repoRoot,
				SessionID:  state.sessionID,
				TurnID:     firstString(record, "id", "turnId", "turn_id"),
				Timestamp:  timestampOf(record, nil),
				SourcePath: source,
			})
		}
	}
	for _, file := range geminiEditedFiles(record["toolCalls"]) {
		state.addEditedFile(file)
	}
}

func scanGeminiChat(path, repoRoot string) ([]Request, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	sessionID := stringValue(root, "sessionId")
	messages, _ := root["messages"].([]any)
	state := parseState{agent: "gemini", repoRoot: repoRoot, sessionID: sessionID, repoMatched: true}
	for _, item := range messages {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if record["type"] == "user" {
			message := geminiContentText(record["content"])
			if message == "" {
				continue
			}
			state.addRequest(Request{
				Agent:      "gemini",
				Model:      state.model,
				Message:    message,
				RepoRoot:   repoRoot,
				SessionID:  sessionID,
				TurnID:     stringValue(record, "id"),
				Timestamp:  timestampOf(record, nil),
				SourcePath: path,
			})
			continue
		}
		if model := stringValue(record, "model"); model != "" {
			state.model = model
			if state.current != nil && state.current.Model == "" {
				state.current.Model = model
			}
		}
		for _, file := range geminiEditedFiles(record["toolCalls"]) {
			state.addEditedFile(file)
		}
	}
	for i := range state.requests {
		finalizeRequest(&state.requests[i])
	}
	return state.requests, nil
}

func updateCodexContext(payload map[string]any, state *parseState) {
	if cwd := cleanAbsPath(stringValue(payload, "cwd")); cwd != "" {
		state.cwd = cwd
		state.repoMatched = cwd == state.repoRoot
	}
	if roots := workspaceRoots(payload["workspace_roots"]); len(roots) > 0 {
		for _, root := range roots {
			if root == state.repoRoot {
				state.repoMatched = true
				break
			}
		}
	}
	if turnID := stringValue(payload, "turn_id"); turnID != "" {
		state.turnID = turnID
	}
	if model := stringValue(payload, "model"); model != "" {
		state.model = model
		if state.current != nil && state.current.Model == "" {
			state.current.Model = model
		}
	}
}

func (s *parseState) addRequest(req Request) {
	if req.Agent == "" {
		req.Agent = s.agent
	}
	finalizeRequest(&req)
	s.requests = append(s.requests, req)
	s.current = &s.requests[len(s.requests)-1]
}

func (s *parseState) addEditedFile(path string) {
	if s.current == nil {
		return
	}
	path = normalizeTranscriptPath(path, s.repoRoot)
	if path == "" {
		return
	}
	for _, existing := range s.current.EditedFiles {
		if existing == path {
			return
		}
	}
	s.current.EditedFiles = append(s.current.EditedFiles, path)
}

func finalizeRequest(req *Request) {
	req.Agent = strings.TrimSpace(req.Agent)
	req.Model = strings.TrimSpace(req.Model)
	req.Message = strings.TrimSpace(req.Message)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.TurnID = strings.TrimSpace(req.TurnID)
	req.Timestamp = strings.TrimSpace(req.Timestamp)
	sort.Strings(req.EditedFiles)
	if req.ID == "" {
		sum := sha1.Sum([]byte(strings.Join([]string{req.Agent, req.SessionID, req.TurnID, req.Timestamp, req.Message, req.SourcePath}, "\x00")))
		req.ID = hex.EncodeToString(sum[:])[:16]
	}
}

func claudeUserText(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var parts []string
		for _, item := range v {
			obj, ok := item.(map[string]any)
			if !ok || obj["type"] != "text" {
				continue
			}
			if text := strings.TrimSpace(stringValue(obj, "text")); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n\n"))
	default:
		return ""
	}
}

func claudeEditedFiles(content any) []string {
	var files []string
	items, _ := content.([]any)
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok || obj["type"] != "tool_use" {
			continue
		}
		name := strings.ToLower(stringValue(obj, "name"))
		if name != "edit" && name != "multiedit" && name != "write" {
			continue
		}
		input := objectValue(obj, "input")
		if file := stringValue(input, "file_path"); file != "" {
			files = append(files, file)
		}
	}
	return files
}

func codexEditedFiles(payload map[string]any) []string {
	if payload["type"] != "function_call" {
		return nil
	}
	name := strings.ToLower(stringValue(payload, "name"))
	args := stringValue(payload, "arguments")
	switch name {
	case "apply_patch":
		return patchFiles(args)
	case "exec_command":
		var parsed map[string]any
		if json.Unmarshal([]byte(args), &parsed) != nil {
			return nil
		}
		return patchFiles(stringValue(parsed, "cmd"))
	default:
		return nil
	}
}

func geminiEditedFiles(toolCalls any) []string {
	items, _ := toolCalls.([]any)
	var files []string
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.ToLower(firstString(obj, "name", "functionName"))
		args := objectValue(obj, "args")
		if len(args) == 0 {
			args = objectValue(obj, "parameters")
		}
		if name == "write_file" || name == "replace" || name == "edit" {
			if file := firstString(args, "file_path", "path", "absolute_path"); file != "" {
				files = append(files, file)
			}
		}
	}
	return files
}

func patchFiles(text string) []string {
	var files []string
	for _, line := range strings.Split(text, "\n") {
		for _, prefix := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: "} {
			if strings.HasPrefix(line, prefix) {
				files = append(files, strings.TrimSpace(strings.TrimPrefix(line, prefix)))
			}
		}
	}
	return files
}

func geminiContentText(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var parts []string
		for _, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text := strings.TrimSpace(stringValue(obj, "text")); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n\n"))
	default:
		return ""
	}
}

func jsonFiles(root, suffix string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), suffix) {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func filterCodexSessionPaths(ctx context.Context, paths []string, repoRoot string, cache *Cache) []string {
	if len(paths) == 0 {
		return paths
	}
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if cache != nil && cache.hasCurrent("codex-jsonl\x00"+path) {
			filtered = append(filtered, path)
			continue
		}
		match, known := codexSessionReferencesRepo(ctx, path, repoRoot)
		if !known || match {
			filtered = append(filtered, path)
		}
	}
	return filtered
}

func codexSessionReferencesRepo(ctx context.Context, path, repoRoot string) (bool, bool) {
	file, err := os.Open(path)
	if err != nil {
		return true, false
	}
	defer file.Close()
	known := false
	reader := bufio.NewReader(file)
	for {
		if ctx.Err() != nil {
			return true, false
		}
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return true, false
		}
		line = strings.TrimSpace(line)
		if line != "" {
			var record map[string]any
			if err := json.Unmarshal([]byte(line), &record); err == nil {
				payload := objectValue(record, "payload")
				if record["type"] == "session_meta" || record["type"] == "turn_context" {
					if codexPayloadReferencesRepo(payload, repoRoot) {
						return true, true
					}
					if stringValue(payload, "cwd") != "" || payload["workspace_roots"] != nil {
						known = true
					}
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	return false, known
}

func codexPayloadReferencesRepo(payload map[string]any, repoRoot string) bool {
	if cwd := cleanAbsPath(stringValue(payload, "cwd")); cwd == repoRoot {
		return true
	}
	for _, root := range workspaceRoots(payload["workspace_roots"]) {
		if root == repoRoot {
			return true
		}
	}
	return false
}

func filterGeminiJSONLPaths(paths []string, geminiHome, projectSlug string) []string {
	if len(paths) == 0 || projectSlug == "" {
		return paths
	}
	projectTmpRoot := filepath.Join(geminiHome, "tmp")
	projectRoot := filepath.Join(projectTmpRoot, projectSlug)
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if rel, err := filepath.Rel(projectTmpRoot, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			if relProject, relErr := filepath.Rel(projectRoot, path); relErr == nil && relProject != "." && !strings.HasPrefix(relProject, "..") {
				filtered = append(filtered, path)
			}
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func geminiProjectSlug(geminiHome, repoRoot string) string {
	raw, err := os.ReadFile(filepath.Join(geminiHome, "projects.json"))
	if err != nil {
		return ""
	}
	var parsed struct {
		Projects map[string]string `json:"projects"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return ""
	}
	return parsed.Projects[repoRoot]
}

func (c *Cache) hasCurrent(key string) bool {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return false
	}
	info, err := os.Stat(parts[1])
	if err != nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.files[key]
	return ok && item.mtime == info.ModTime().UnixNano() && item.size == info.Size()
}

func workspaceRoots(value any) []string {
	items, _ := value.([]any)
	var roots []string
	for _, item := range items {
		switch v := item.(type) {
		case string:
			if path := cleanAbsPath(v); path != "" {
				roots = append(roots, path)
			}
		case map[string]any:
			if path := cleanAbsPath(stringValue(v, "root")); path != "" {
				roots = append(roots, path)
			}
		}
	}
	return roots
}

func timestampOf(record, payload map[string]any) string {
	for _, obj := range []map[string]any{record, payload} {
		if obj == nil {
			continue
		}
		if value := firstString(obj, "timestamp", "started_at", "startTime"); value != "" {
			return value
		}
	}
	return ""
}

func normalizeTranscriptPath(path, repoRoot string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return ""
		}
		return filepath.ToSlash(rel)
	}
	if strings.HasPrefix(path, ".."+string(filepath.Separator)) || path == ".." {
		return ""
	}
	return filepath.ToSlash(path)
}

func objectValue(obj map[string]any, key string) map[string]any {
	value, _ := obj[key].(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func stringValue(obj map[string]any, key string) string {
	value, _ := obj[key].(string)
	return value
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(obj, key); value != "" {
			return value
		}
	}
	return ""
}

func cleanAbsPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func escapeClaudeProjectPath(path string) string {
	clean := filepath.Clean(path)
	return strings.ReplaceAll(clean, string(filepath.Separator), "-")
}

func codexHomeDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("CODEX_HOME")); override != "" {
		return filepath.Abs(expandHome(override))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

func claudeHomeDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); override != "" {
		return filepath.Abs(expandHome(override))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

func expandHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
