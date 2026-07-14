// Command load is the argo-watcher e2e soak driver.
//
// It fires authenticated deploys concurrently across a set of fixture apps that
// all share one gitops repo, while holding WebSocket clients open. A separate
// competitor writer (scripts/competitor.sh) pushes to the same repo throughout
// the run, so argo-watcher's (per-repo-serialized) write-back keeps hitting
// non-fast-forward rejections and exercises its retry loop — the git-conflict
// scenario the lab targets.
//
// Deploys are serialized PER APP (a per-app lock), so no task supersedes
// another; every submitted task is expected to reach "deployed". Each submitted
// deploy always runs to a terminal status on its own budget (even past the run
// deadline), so the last recorded tag per app matches the git override file and
// the no-lost-update check is race-free. The driver prints a JSON summary that
// the collect step checks against the repo.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"gopkg.in/yaml.v3"

	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/updater"
)

type config struct {
	baseURL    string
	wsURL      string
	token      string
	apps       int
	workers    int
	wsClients  int
	duration   time.Duration
	tags       []string
	pollEvery  time.Duration
	taskBudget time.Duration
}

type summary struct {
	Submitted int               `json:"submitted"`
	Deployed  int               `json:"deployed"`
	Failed    int               `json:"failed"`
	Other     map[string]int    `json:"other"`
	LastTag   map[string]string `json:"last_tag"` // app -> last successfully deployed tag
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func loadConfig() config {
	dur, _ := time.ParseDuration(env("DURATION", "5m"))
	return config{
		baseURL:    strings.TrimRight(env("BASE_URL", "http://localhost:8080"), "/"),
		wsURL:      env("WS_URL", "ws://localhost:8080/ws"),
		token:      env("DEPLOY_TOKEN", "e2e-deploy-token"),
		apps:       envInt("APPS", 5),
		workers:    envInt("WORKERS", 10),
		wsClients:  envInt("WS_CLIENTS", 10),
		duration:   dur,
		tags:       strings.Split(env("TAGS", "v1.10.1,v1.10.2,v1.10.3"), ","),
		pollEvery:  2 * time.Second,
		taskBudget: 3 * time.Minute,
	}
}

func main() {
	cfg := loadConfig()

	// MODE=race runs the same-app supersession scenario instead of the soak: fire
	// concurrent deploys to ONE app with distinct tags and assert the committed
	// tag belongs to a task that actually deployed — never a superseded one that
	// clobbered the winner. Verifies models.ErrDeploymentSuperseded end-to-end.
	if os.Getenv("MODE") == "race" {
		runRace(cfg)
		return
	}

	log.Printf("driver: apps=%d workers=%d wsClients=%d duration=%s", cfg.apps, cfg.workers, cfg.wsClients, cfg.duration)

	// runCtx bounds the WebSocket clients and the "keep starting deploys" window.
	runCtx, cancel := context.WithTimeout(context.Background(), cfg.duration)
	defer cancel()

	var wg sync.WaitGroup

	// Hold WebSocket clients open for the whole run (fan-out + hijack/shutdown
	// surface). Failures here are logged, not fatal — the deploy load is the gate.
	for i := 0; i < cfg.wsClients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runWSClient(runCtx, cfg.wsURL)
		}()
	}

	// Per-app locks serialize deploys to each app (no supersession).
	appLocks := make([]sync.Mutex, cfg.apps)
	appTagIdx := make([]int, cfg.apps)

	var mu sync.Mutex
	sum := summary{Other: map[string]int{}, LastTag: map[string]string{}}
	record := func(app, tag, status string) {
		mu.Lock()
		defer mu.Unlock()
		sum.Submitted++
		switch status {
		case models.StatusDeployedMessage:
			sum.Deployed++
			sum.LastTag[app] = tag
		case models.StatusFailedMessage, models.StatusArgoCDUnavailableMessage:
			sum.Failed++
		default:
			sum.Other[status]++
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	// Fixed seed keeps app/tag selection reproducible across runs.
	rng := rand.New(rand.NewSource(1)) // NOSONAR - load-driver work selection, not a security context
	var rngMu sync.Mutex

	for w := 0; w < cfg.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Stop STARTING deploys once the run window closes, but always let a
			// submitted deploy finish (below) so git and last_tag stay in sync.
			for runCtx.Err() == nil {
				rngMu.Lock()
				app := rng.Intn(cfg.apps)
				rngMu.Unlock()

				appLocks[app].Lock()
				appTagIdx[app] = (appTagIdx[app] + 1) % len(cfg.tags)
				tag := cfg.tags[appTagIdx[app]]
				appName := fmt.Sprintf("app%d", app+1)

				// Fresh per-task context (NOT runCtx): a submitted deploy always
				// runs to a terminal status, so argo-watcher never commits a tag
				// the driver failed to record.
				taskCtx, tcancel := context.WithTimeout(context.Background(), cfg.taskBudget)
				status := deployAndWait(taskCtx, client, cfg, appName, tag)
				tcancel()

				record(appName, tag, status)
				appLocks[app].Unlock()
			}
		}()
	}

	wg.Wait()

	out, _ := json.MarshalIndent(sum, "", "  ")
	fmt.Println(string(out))
	if sum.Failed > 0 || len(sum.Other) > 0 {
		log.Fatalf("driver: %d failed, non-deployed outcomes: %v", sum.Failed, sum.Other)
	}
}

// deployAndWait submits one authenticated deploy and polls to a terminal status,
// bounded by ctx (the per-task budget).
func deployAndWait(ctx context.Context, client *http.Client, cfg config, app, tag string) string {
	task := models.Task{
		App:     app,
		Author:  "e2e-driver",
		Project: "lab",
		Images:  []models.Image{{Image: "traefik/whoami", Tag: tag}},
	}
	body, _ := json.Marshal(task)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, cfg.baseURL+"/api/v1/tasks", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ARGO_WATCHER_DEPLOY_TOKEN", cfg.token)

	resp, err := client.Do(req)
	if err != nil {
		return "submit-error"
	}
	var created models.TaskStatus
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.Id == "" {
		return "no-id"
	}

	for ctx.Err() == nil {
		st := getStatus(ctx, client, cfg.baseURL, created.Id)
		switch st {
		case models.StatusDeployedMessage, models.StatusFailedMessage,
			models.StatusAborted, models.StatusCancelledMessage,
			models.StatusArgoCDUnavailableMessage:
			return st
		}
		time.Sleep(cfg.pollEvery)
	}
	return "timeout"
}

func getStatus(ctx context.Context, client *http.Client, baseURL, id string) string {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/tasks/"+id, nil)
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var ts models.TaskStatus
	if err := json.NewDecoder(resp.Body).Decode(&ts); err != nil {
		return ""
	}
	return ts.Status
}

// runRace fires an OLDER then a NEWER deploy for the same app while a competitor
// keeps write-backs retrying, and asserts the NEWER tag is what ends up committed
// — i.e. the older task's retry never clobbers the newer one. Without the
// supersession guard in the write-back loop, the older task could commit its
// (stale) tag last. Fatals on violation.
func runRace(cfg config) {
	const app = "app1"
	if len(cfg.tags) < 2 {
		log.Fatalf("race: need >= 2 distinct tags, got %v", cfg.tags)
	}
	tagOld, tagNew := cfg.tags[0], cfg.tags[len(cfg.tags)-1]
	if tagOld == tagNew {
		log.Fatalf("race: first and last tag must differ (%v)", cfg.tags)
	}
	log.Printf("race: %s <- OLD %s then NEW %s (competitor forces retries)", app, tagOld, tagNew)

	client := &http.Client{Timeout: 30 * time.Second}
	var mu sync.Mutex
	status := map[string]string{}
	var wg sync.WaitGroup
	deploy := func(tag string) {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), cfg.taskBudget)
		defer cancel()
		st := deployAndWait(ctx, client, cfg, app, tag)
		mu.Lock()
		status[tag] = st
		mu.Unlock()
	}

	wg.Add(1)
	go deploy(tagOld)
	time.Sleep(300 * time.Millisecond) // ensure NEW is created after OLD (supersedes it)
	wg.Add(1)
	go deploy(tagNew)
	wg.Wait()

	gitTag, err := readGitTag(app)
	if err != nil {
		log.Fatalf("race: reading committed tag: %v", err)
	}
	log.Printf("race: OLD %s=%s  NEW %s=%s  committed git tag=%s",
		tagOld, status[tagOld], tagNew, status[tagNew], gitTag)

	for tag, st := range status {
		if st == models.StatusFailedMessage || st == models.StatusArgoCDUnavailableMessage {
			log.Fatalf("race: FAIL — deploy %s ended %q", tag, st)
		}
	}
	if gitTag == tagOld {
		log.Fatalf("race: FAIL — the OLDER tag %q won over the newer %q (superseded task clobbered the winner)", tagOld, tagNew)
	}
	if gitTag != tagNew {
		log.Fatalf("race: FAIL — committed tag %q is neither the old nor the new tag", gitTag)
	}
	fmt.Printf("race OK: newer tag %q won; older %q did not clobber it\n", tagNew, tagOld)
}

// readGitTag clones the gitops repo (GITEA_REPO_URL) and returns the app.image.tag
// currently committed in the app's .argocd-source override file.
func readGitTag(app string) (string, error) {
	repoURL := os.Getenv("GITEA_REPO_URL")
	if repoURL == "" {
		return "", fmt.Errorf("GITEA_REPO_URL not set")
	}
	dir, err := os.MkdirTemp("", "race-git-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if out, err := exec.Command("git", "clone", "-q", repoURL, dir).CombinedOutput(); err != nil { // NOSONAR - local dev lab; git resolved from the developer's trusted PATH
		return "", fmt.Errorf("git clone: %w: %s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "chart", fmt.Sprintf(".argocd-source-%s.yaml", app)))
	if err != nil {
		return "", err
	}
	var ov updater.ArgoOverrideFile
	if err := yaml.Unmarshal(data, &ov); err != nil {
		return "", err
	}
	for _, p := range ov.Helm.Parameters {
		if p.Name == "app.image.tag" {
			return p.Value, nil
		}
	}
	return "", fmt.Errorf("app.image.tag not found in %s override", app)
}

// runWSClient keeps one WebSocket connection open, draining messages until the
// run context ends. Reconnects on drop so a client is held for the whole soak.
func runWSClient(ctx context.Context, wsURL string) {
	for ctx.Err() == nil {
		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		for ctx.Err() == nil {
			if _, _, err := conn.Read(ctx); err != nil {
				break
			}
		}
		conn.Close(websocket.StatusNormalClosure, "")
	}
}
