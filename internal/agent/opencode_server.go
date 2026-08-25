package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const ocTimeout = 15 * time.Second

type OpenCodeServer struct {
	ProjectPath string
	BaseURL     string

	p        *proc
	ctx      context.Context
	cancel   context.CancelFunc
	client   *http.Client
	mu       sync.Mutex
	subs     map[string]map[chan AgentEvent]struct{}
	shutdown bool
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func StartOpenCodeServer(binPath, projectPath, configFile string) (*OpenCodeServer, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{"serve", "--hostname", "127.0.0.1", "--port", fmt.Sprintf("%d", port)}
	var extraEnv []string
	if configFile != "" {
		extraEnv = []string{"OPENCODE_CONFIG=" + configFile}
	}
	p, err := startProcEnv(ctx, binPath, args, projectPath, extraEnv)
	if err != nil {
		cancel()
		return nil, err
	}
	s := &OpenCodeServer{
		ProjectPath: projectPath,
		BaseURL:     fmt.Sprintf("http://127.0.0.1:%d", port),
		p:           p,
		ctx:         ctx,
		cancel:      cancel,
		client:      &http.Client{Timeout: ocTimeout},
		subs:        map[string]map[chan AgentEvent]struct{}{},
	}
	if err := s.waitHealthy(25 * time.Second); err != nil {
		p.kill()
		cancel()
		return nil, fmt.Errorf("opencode serve failed: %w | %s", err, p.stderr.Tail(2048))
	}
	go s.sseLoop()
	return s, nil
}

func (s *OpenCodeServer) waitHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := s.BaseURL + "/global/health"
	for time.Now().Before(deadline) {
		select {
		case <-s.ctx.Done():
			return fmt.Errorf("server exited during startup")
		default:
		}
		resp, err := s.client.Get(url)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			var h struct {
				Healthy bool `json:"healthy"`
			}
			if json.Unmarshal(body, &h) == nil && h.Healthy {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("health check timed out")
}

func (s *OpenCodeServer) sseLoop() {
	for s.ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(s.ctx, http.MethodGet, s.BaseURL+"/event", nil)
		resp, err := s.client.Do(req)
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)
		var data bytes.Buffer
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "data:"):
				data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			case line == "" && data.Len() > 0:
				s.dispatch(data.Bytes())
				data.Reset()
			}
		}
		resp.Body.Close()
		if s.ctx.Err() == nil {
			time.Sleep(time.Second)
		}
	}
}

func (s *OpenCodeServer) dispatch(raw []byte) {
	for _, ev := range ParseOpenCodeEvent(raw) {
		if ev.SessionID == "" {
			continue
		}
		s.mu.Lock()
		for ch := range s.subs[ev.SessionID] {
			select {
			case ch <- ev:
			default:
			}
		}
		s.mu.Unlock()
	}
}

func (s *OpenCodeServer) Subscribe(sessionID string) (<-chan AgentEvent, func()) {
	ch := make(chan AgentEvent, 1024)
	s.mu.Lock()
	if s.subs[sessionID] == nil {
		s.subs[sessionID] = map[chan AgentEvent]struct{}{}
	}
	s.subs[sessionID][ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs[sessionID], ch)
		if len(s.subs[sessionID]) == 0 {
			delete(s.subs, sessionID)
		}
		s.mu.Unlock()
		close(ch)
	}
}

func (s *OpenCodeServer) do(method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(s.ctx, method, s.BaseURL+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("opencode %s %s: %d %s", method, path, resp.StatusCode, truncateStr(string(data), 400))
	}
	return data, nil
}

func (s *OpenCodeServer) CreateSession() (string, error) {
	data, err := s.do(http.MethodPost, "/session", map[string]any{})
	if err != nil {
		return "", err
	}
	var sess struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &sess); err != nil || sess.ID == "" {
		return "", fmt.Errorf("create session: bad response %q", truncateStr(string(data), 200))
	}
	return sess.ID, nil
}

func (s *OpenCodeServer) PromptAsync(sessionID, text string) error {
	return s.PromptAsyncWithOptions(sessionID, text, TurnOptions{})
}

func (s *OpenCodeServer) PromptAsyncWithOptions(sessionID, text string, options TurnOptions) error {
	body := map[string]any{
		"parts": []map[string]any{{"type": "text", "text": text}},
	}
	if options.Model != "" {
		providerID, modelID, ok := strings.Cut(options.Model, "/")
		if ok && providerID != "" && modelID != "" {
			body["model"] = map[string]string{"providerID": providerID, "modelID": modelID}
		}
	}
	if options.FastMode {
		body["variant"] = "fast"
	} else if options.ReasoningEffort != "" {
		body["variant"] = options.ReasoningEffort
	}
	_, err := s.do(http.MethodPost, "/session/"+sessionID+"/prompt_async", body)
	return err
}

func (s *OpenCodeServer) Abort(sessionID string) error {
	_, err := s.do(http.MethodPost, "/session/"+sessionID+"/abort", map[string]any{})
	return err
}

func (s *OpenCodeServer) Shutdown() {
	s.mu.Lock()
	already := s.shutdown
	s.shutdown = true
	s.mu.Unlock()
	if already {
		return
	}
	s.p.kill()
	s.cancel()
}
