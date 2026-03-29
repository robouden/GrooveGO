// Package codeberg receives Codeberg (Gitea) webhook events and publishes
// them as messages into a GrooveGO workspace channel.
package codeberg

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Publisher is anything that can publish a plain-text message to a channel.
type Publisher interface {
	Publish(ctx context.Context, body string) error
}

// Handler handles incoming Codeberg webhook requests.
type Handler struct {
	secret  string    // HMAC secret set in Codeberg webhook config (optional)
	channel Publisher // target GrooveGO channel
}

// New creates a Handler. secret may be empty to skip signature verification.
func New(secret string, channel Publisher) *Handler {
	return &Handler{secret: secret, channel: channel}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB max
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}

	if h.secret != "" {
		sig := r.Header.Get("X-Gitea-Signature")
		if !verifySignature(h.secret, body, sig) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	event := r.Header.Get("X-Gitea-Event")
	msg := format(event, body)
	if msg == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	_ = h.channel.Publish(r.Context(), msg)
	w.WriteHeader(http.StatusOK)
}

// ── payload structs (only the fields we need) ────────────────────────────────

type repo struct {
	FullName string `json:"full_name"`
}
type user struct {
	Login string `json:"login"`
}
type commit struct {
	Message string `json:"message"`
	URL     string `json:"url"`
}
type issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	HTMLURL string `json:"html_url"`
}
type comment struct {
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}
type release struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
}

type pushPayload struct {
	Ref     string  `json:"ref"`
	Commits []commit `json:"commits"`
	Repo    repo    `json:"repository"`
	Pusher  user    `json:"pusher"`
}
type issuePayload struct {
	Action string `json:"action"`
	Issue  issue  `json:"issue"`
	Repo   repo   `json:"repository"`
	Sender user   `json:"sender"`
}
type prPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
	} `json:"pull_request"`
	Repo   repo `json:"repository"`
	Sender user `json:"sender"`
}
type commentPayload struct {
	Action  string  `json:"action"`
	Issue   issue   `json:"issue"`
	Comment comment `json:"comment"`
	Repo    repo    `json:"repository"`
	Sender  user    `json:"sender"`
}
type releasePayload struct {
	Action  string  `json:"action"`
	Release release `json:"release"`
	Repo    repo    `json:"repository"`
	Sender  user    `json:"sender"`
}

// ── formatting ───────────────────────────────────────────────────────────────

func format(event string, body []byte) string {
	switch event {
	case "push":
		var p pushPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return ""
		}
		branch := strings.TrimPrefix(p.Ref, "refs/heads/")
		n := len(p.Commits)
		if n == 0 {
			return ""
		}
		first := shorten(p.Commits[0].Message)
		if n == 1 {
			return fmt.Sprintf("[push] %s pushed to %s/%s: %s", p.Pusher.Login, p.Repo.FullName, branch, first)
		}
		return fmt.Sprintf("[push] %s pushed %d commits to %s/%s: %s (+%d more)", p.Pusher.Login, n, p.Repo.FullName, branch, first, n-1)

	case "issues":
		var p issuePayload
		if err := json.Unmarshal(body, &p); err != nil {
			return ""
		}
		if p.Action != "opened" && p.Action != "closed" && p.Action != "reopened" {
			return ""
		}
		return fmt.Sprintf("[issue] %s %s #%d in %s: %s — %s", p.Sender.Login, p.Action, p.Issue.Number, p.Repo.FullName, p.Issue.Title, p.Issue.HTMLURL)

	case "pull_request":
		var p prPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return ""
		}
		if p.Action != "opened" && p.Action != "closed" && p.Action != "merged" {
			return ""
		}
		return fmt.Sprintf("[PR] %s %s #%d in %s: %s — %s", p.Sender.Login, p.Action, p.Number, p.Repo.FullName, p.PullRequest.Title, p.PullRequest.HTMLURL)

	case "issue_comment":
		var p commentPayload
		if err := json.Unmarshal(body, &p); err != nil {
			return ""
		}
		if p.Action != "created" {
			return ""
		}
		return fmt.Sprintf("[comment] %s commented on #%d in %s: %s — %s", p.Sender.Login, p.Issue.Number, p.Repo.FullName, shorten(p.Comment.Body), p.Comment.HTMLURL)

	case "release":
		var p releasePayload
		if err := json.Unmarshal(body, &p); err != nil {
			return ""
		}
		if p.Action != "published" {
			return ""
		}
		name := p.Release.Name
		if name == "" {
			name = p.Release.TagName
		}
		return fmt.Sprintf("[release] %s released %s in %s — %s", p.Sender.Login, name, p.Repo.FullName, p.Release.HTMLURL)
	}
	return ""
}

func shorten(s string) string {
	s = strings.SplitN(s, "\n", 2)[0] // first line only
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

func verifySignature(secret string, body []byte, sig string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}
