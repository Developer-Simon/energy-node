// Package tinytuya provides the dashboard's narrow process boundary to the
// Python TinyTuya installation on the target system.
package tinytuya

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const maxOutputBytes = 2 << 20

type Prober interface {
	Devices(context.Context, CloudRequest) ([]Device, error)
	Status(context.Context, StatusRequest) (StatusResult, error)
}

type Client struct {
	python  string
	script  string
	timeout time.Duration
	mu      sync.Mutex
}

type CloudRequest struct {
	Region       string `json:"region"`
	AccessID     string `json:"access_id"`
	AccessSecret string `json:"access_secret"`
}

type StatusRequest struct {
	DeviceID string  `json:"device_id"`
	LocalKey string  `json:"local_key"`
	IP       string  `json:"ip"`
	Version  float64 `json:"version"`
}

type Device struct {
	DeviceID string  `json:"device_id"`
	Name     string  `json:"name"`
	LocalKey string  `json:"local_key"`
	IP       string  `json:"ip"`
	Version  float64 `json:"version"`
}

type DPS struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type StatusResult struct {
	DPS []DPS          `json:"dps"`
	Raw map[string]any `json:"raw"`
}

type helperResponse struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error"`
	Devices json.RawMessage `json:"devices"`
	Status  json.RawMessage `json:"status"`
}

func NewClient(python, script string, timeout time.Duration) *Client {
	if python == "" {
		python = "/usr/bin/python3"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{python: python, script: script, timeout: timeout}
}

func (c *Client) Devices(ctx context.Context, request CloudRequest) ([]Device, error) {
	if err := validateCloudRequest(request); err != nil {
		return nil, err
	}
	var envelope helperResponse
	if err := c.run(ctx, map[string]any{"operation": "devices", "request": request}, &envelope); err != nil {
		return nil, err
	}
	var response []Device
	if err := json.Unmarshal(envelope.Devices, &response); err != nil {
		return nil, errors.New("TinyTuya-Helper lieferte keine gültige Geräteliste")
	}
	return response, nil
}

func (c *Client) Status(ctx context.Context, request StatusRequest) (StatusResult, error) {
	if err := validateStatusRequest(request); err != nil {
		return StatusResult{}, err
	}
	var envelope helperResponse
	if err := c.run(ctx, map[string]any{"operation": "status", "request": request}, &envelope); err != nil {
		return StatusResult{}, err
	}
	var response StatusResult
	if err := json.Unmarshal(envelope.Status, &response); err != nil {
		return StatusResult{}, errors.New("TinyTuya-Helper lieferte keinen gültigen Gerätestatus")
	}
	return response, nil
}

func (c *Client) run(parent context.Context, request any, target any) error {
	if c.script == "" {
		return errors.New("TinyTuya-Helper ist nicht konfiguriert")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()

	c.mu.Lock()
	defer c.mu.Unlock()
	command := exec.CommandContext(ctx, c.python, c.script)
	command.Stdin = bytes.NewReader(payload)
	var stdout limitedBuffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("TinyTuya-Abfrage überschritt das Zeitlimit")
		}
		return errors.New("TinyTuya-Helper konnte nicht ausgeführt werden")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("TinyTuya-Abfrage überschritt das Zeitlimit")
	}
	var envelope helperResponse
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		return errors.New("TinyTuya-Helper lieferte ungültiges JSON")
	}
	if !envelope.OK {
		message := strings.TrimSpace(envelope.Error)
		if message == "" || strings.Contains(strings.ToLower(message), "secret") {
			message = "TinyTuya-Abfrage fehlgeschlagen"
		}
		return errors.New(message)
	}
	if err := json.Unmarshal(stdout.Bytes(), target); err != nil {
		return fmt.Errorf("TinyTuya-Helper-Antwort konnte nicht gelesen werden: %w", err)
	}
	return nil
}

func validateCloudRequest(request CloudRequest) error {
	if request.Region == "" || request.AccessID == "" || request.AccessSecret == "" {
		return errors.New("Region, Access ID und Access Secret sind erforderlich")
	}
	return nil
}

func validateStatusRequest(request StatusRequest) error {
	if request.DeviceID == "" || request.LocalKey == "" || request.IP == "" {
		return errors.New("Device ID, Local Key und IP-Adresse sind erforderlich")
	}
	if request.Version < 3 {
		return errors.New("die Protokollversion muss mindestens 3.0 sein")
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer
	limited bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > maxOutputBytes {
		b.limited = true
		return 0, errors.New("TinyTuya-Helper-Antwort ist zu groß")
	}
	return b.Buffer.Write(data)
}
