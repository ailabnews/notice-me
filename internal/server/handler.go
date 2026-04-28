package server

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"notify-me/internal/config"
	"notify-me/internal/dispatcher"
)

type jsonBody struct {
	Title      *string `json:"title,omitempty"`
	Message    *string `json:"message,omitempty"`
	OkText     *string `json:"ok_text,omitempty"`
	CancelText *string `json:"cancel_text,omitempty"`
	Timeout    *int    `json:"timeout,omitempty"`
}

// parseRequest builds a Notification from the HTTP request, applying the
// priority: JSON > Header > Query > endpoint default > global default.
func parseRequest(r *http.Request, ep config.EndpointConfig, defaultTimeoutSeconds int) (*dispatcher.Notification, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10)) // 64KB cap
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	n := dispatcher.NewNotification()
	n.Endpoint = ep.Path
	n.Title = ep.Title
	n.Message = string(raw)
	n.OkText = ep.OkText
	n.CancelText = ep.CancelText
	n.Mode = dispatcher.Mode(ep.Mode)
	n.Timeout = time.Duration(defaultTimeoutSeconds) * time.Second
	n.SourceIP = r.RemoteAddr
	n.SourceHdr = r.Header.Get("X-Source")

	// Query layer
	q := r.URL.Query()
	if v := q.Get("title"); v != "" {
		n.Title = v
	}
	if v := q.Get("ok"); v != "" {
		n.OkText = v
	}
	if v := q.Get("cancel"); v != "" {
		n.CancelText = v
	}
	if v := q.Get("timeout"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s > 0 {
			n.Timeout = time.Duration(s) * time.Second
		}
	}

	// Header layer
	if v := r.Header.Get("X-Title"); v != "" {
		n.Title = v
	}
	if v := r.Header.Get("X-Ok"); v != "" {
		n.OkText = v
	}
	if v := r.Header.Get("X-Cancel"); v != "" {
		n.CancelText = v
	}
	if v := r.Header.Get("X-Timeout"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s > 0 {
			n.Timeout = time.Duration(s) * time.Second
		}
	}

	// JSON layer (top priority). Only attempt when Content-Type's media type
	// is exactly application/json (mime.ParseMediaType strips charset etc;
	// excludes application/json-patch+json and similar variants).
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if len(raw) > 0 && mediaType == "application/json" {
		var body jsonBody
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, fmt.Errorf("parse json: %w", err)
		}
		if body.Title != nil {
			n.Title = *body.Title
		}
		if body.Message != nil {
			n.Message = *body.Message
		}
		if body.OkText != nil {
			n.OkText = *body.OkText
		}
		if body.CancelText != nil {
			n.CancelText = *body.CancelText
		}
		if body.Timeout != nil && *body.Timeout > 0 {
			n.Timeout = time.Duration(*body.Timeout) * time.Second
		}
	}
	return n, nil
}
