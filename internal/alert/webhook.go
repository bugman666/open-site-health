package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const userAgent = "open-site-health/0.1"

func (d *Dispatcher) postWebhook(ctx context.Context, ev Event, now time.Time) error {
	body, err := json.Marshal(Payload{
		Service:  "open-site-health",
		TargetID: ev.TargetID,
		URL:      ev.URL,
		Kind:     ev.Kind,
		Message:  ev.Message,
		SentAt:   now.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.cfg.Alert.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", userAgent)

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
