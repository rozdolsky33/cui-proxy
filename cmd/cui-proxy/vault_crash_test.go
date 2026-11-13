package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVaultCrashHelper(t *testing.T) {
	if os.Getenv("CUI_VAULT_RECOVERY_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	a, err := NewApp(AppOptions{ConfigPath: os.Getenv("CUI_VAULT_RECOVERY_CONFIG"), saveInterval: time.Hour, Logf: func(string, ...any) {}})
	if err != nil {
		os.Exit(2)
	}
	if err = a.Start(); err != nil {
		os.Exit(3)
	}
	px, _ := a.Addrs()
	if err = os.WriteFile(os.Getenv("CUI_VAULT_RECOVERY_READY"), []byte(px), 0600); err != nil {
		os.Exit(4)
	}
	select {}
}

func startCrashApp(t *testing.T, cfg string) (string, func()) {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestVaultCrashHelper$")
	cmd.Env = append(os.Environ(), "CUI_VAULT_RECOVERY_HELPER=1", "CUI_VAULT_RECOVERY_CONFIG="+cfg, "CUI_VAULT_RECOVERY_READY="+ready)
	var captured bytes.Buffer
	cmd.Stdout = &captured
	cmd.Stderr = &captured
	if err := cmd.Start(); err != nil {
		t.Fatal("subprocess start failed")
	}
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}
	t.Cleanup(stop)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(ready); err == nil && len(b) > 0 {
			return string(b), stop
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("subprocess startup deadline exceeded")
	return "", stop
}

func TestFileVaultAbruptRestartRestoresExposedMapping(t *testing.T) {
	up := newUpstream(t, nil)
	_, cfg := testDir(t, cfgSpec{Upstream: up.url(), Vault: fileVault})
	px, crash := startCrashApp(t, cfg)
	status, got := ask(t, px, realIP)
	if status != http.StatusOK || got != realIP {
		t.Fatal("initial request did not restore")
	}
	synthetic := up.next(t)
	crash() // No Shutdown, Close or periodic save; also releases lifetime flock.
	px2, _ := startCrashApp(t, cfg)
	status, got = ask(t, px2, synthetic)
	if status != http.StatusOK || got != realIP {
		t.Fatal("crash recovery did not restore exposed mapping exactly")
	}
	if up.next(t) != synthetic {
		t.Fatal("resumed history changed mapping")
	}
}

func TestFileVaultAbruptRestartRestoresToolArguments(t *testing.T) {
	captured := make(chan string, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input string `json:"input"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		captured <- body.Input
		args, _ := json.Marshal(map[string]string{"target": body.Input})
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(kind string, fields map[string]any) {
			fields["type"] = kind
			b, _ := json.Marshal(fields)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b)
		}
		emit("response.output_item.added", map[string]any{"output_index": 0, "item": map[string]any{"id": "item", "type": "function_call", "call_id": "call", "name": "probe", "arguments": ""}})
		emit("response.function_call_arguments.delta", map[string]any{"item_id": "item", "output_index": 0, "delta": string(args)})
		emit("response.function_call_arguments.done", map[string]any{"item_id": "item", "output_index": 0, "arguments": string(args)})
	}))
	defer upstream.Close()
	_, cfg := testDir(t, cfgSpec{Upstream: upstream.URL + "/codex", Vault: fileVault})
	px, crash := startCrashApp(t, cfg)
	if askRecoveredTool(t, px, realIP) != realIP {
		t.Fatal("initial tool arguments did not restore")
	}
	synthetic := <-captured
	crash()
	px2, _ := startCrashApp(t, cfg)
	if askRecoveredTool(t, px2, synthetic) != realIP {
		t.Fatal("resumed tool arguments restored incorrectly")
	}
	if <-captured != synthetic {
		t.Fatal("resumed synthetic changed")
	}
}

func askRecoveredTool(t *testing.T, addr, input string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"model": "test", "input": input})
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Post("http://"+addr+"/v1/responses", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal("tool request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("tool request status: %d", res.StatusCode)
	}
	scanner := bufio.NewScanner(res.Body)
	var arguments strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) != nil {
			t.Fatal("invalid tool event")
		}
		if event.Type == "response.function_call_arguments.delta" {
			arguments.WriteString(event.Delta)
		}
	}
	if scanner.Err() != nil {
		t.Fatal("tool stream read failed")
	}
	var args struct {
		Target string `json:"target"`
	}
	if json.Unmarshal([]byte(arguments.String()), &args) != nil {
		t.Fatal("tool arguments invalid")
	}
	return args.Target
}
