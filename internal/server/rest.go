package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	stdtls "crypto/tls"
	"time"

	"github.com/google/uuid"
	"github.com/GrayCodeAI/yaad/internal/bench"
	"github.com/GrayCodeAI/yaad/internal/embeddings"
	"github.com/GrayCodeAI/yaad/internal/engine"
	"github.com/GrayCodeAI/yaad/internal/exportimport"
	"github.com/GrayCodeAI/yaad/internal/graph"
	"github.com/GrayCodeAI/yaad/internal/skill"
	"github.com/GrayCodeAI/yaad/internal/storage"
	"github.com/GrayCodeAI/yaad/internal/team"
	"github.com/GrayCodeAI/yaad/internal/version"
)

const (
	maxRequestBodySize = 1 << 20 // 1 MB
	maxGraphDepth      = 5
)

// RESTServer serves the HTTP API.
type RESTServer struct {
	eng      *engine.Engine
	addr     string
	tlsCfg   *stdtls.Config
	embedder embeddings.Provider // nil = no vector search
	SSE      *SSEBroker
	srv      *http.Server
}

// NewRESTServer creates a REST server.
func NewRESTServer(eng *engine.Engine, addr string) *RESTServer {
	return &RESTServer{eng: eng, addr: addr, SSE: NewSSEBroker()}
}

// WithEmbedder sets the embedding provider for vector search.
func (s *RESTServer) WithEmbedder(p embeddings.Provider) *RESTServer {
	s.embedder = p
	return s
}

// WithTLS sets TLS config on the server.
func (s *RESTServer) WithTLS(cfg *stdtls.Config) *RESTServer {
	s.tlsCfg = cfg
	return s
}

// ListenAndServe starts the HTTP server with middleware.
func (s *RESTServer) ListenAndServe() error {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	wrapped := s.withMiddleware(s.withRateLimit(mux))
	s.srv = &http.Server{
		Addr:         s.addr,
		Handler:      http.TimeoutHandler(wrapped, 30*time.Second, `{"error":"request timeout"}`),
		TLSConfig:    s.tlsCfg,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	if s.tlsCfg != nil {
		fmt.Printf("yaad REST API (HTTPS) listening on %s\n", s.addr)
		ln, err := stdtls.Listen("tcp", s.addr, s.tlsCfg)
		if err != nil {
			return err
		}
		return s.srv.Serve(ln)
	}
	fmt.Printf("yaad REST API listening on %s\n", s.addr)
	return s.srv.ListenAndServe()
}

// Shutdown gracefully shuts down the REST server with a timeout.
func (s *RESTServer) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// withMiddleware wraps the handler with panic recovery and request logging.
func (s *RESTServer) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Limit request body size
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
		// Panic recovery
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("http panic recovered", "path", r.URL.Path, "panic", rec)
				httpErr(w, fmt.Errorf("internal server error"), 500)
			}
		}()
		next.ServeHTTP(w, r)
		slog.Debug("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
	})
}

// RegisterRoutes registers all routes on the given mux (useful for testing).
func (s *RESTServer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /yaad/remember", s.handleRemember)
	mux.HandleFunc("POST /yaad/recall", s.handleRecall)
	mux.HandleFunc("GET /yaad/context", s.handleContext)
	mux.HandleFunc("POST /yaad/link", s.handleLink)
	mux.HandleFunc("DELETE /yaad/link/{id}", s.handleDeleteLink)
	mux.HandleFunc("GET /yaad/node/{id}", s.handleGetNode)
	mux.HandleFunc("GET /yaad/subgraph/{id}", s.handleSubgraph)
	mux.HandleFunc("GET /yaad/impact/{file...}", s.handleImpact)
	mux.HandleFunc("DELETE /yaad/forget/{id}", s.handleForget)
	mux.HandleFunc("GET /yaad/health", s.handleHealth)
	mux.HandleFunc("GET /yaad/version", s.handleVersion)
	mux.HandleFunc("GET /yaad/graph/stats", s.handleStats)
	mux.HandleFunc("GET /yaad/sessions", s.handleSessions)
	mux.HandleFunc("POST /yaad/session/start", s.handleSessionStart)
	mux.HandleFunc("POST /yaad/session/end", s.handleSessionEnd)
	mux.HandleFunc("GET /yaad/stale", s.handleStale)
	mux.HandleFunc("POST /yaad/embed", s.handleEmbed)
	mux.HandleFunc("POST /yaad/hybrid-recall", s.handleHybridRecall)
	mux.HandleFunc("GET /yaad/proactive", s.handleProactive)
	mux.HandleFunc("POST /yaad/feedback", s.handleFeedback)
	mux.HandleFunc("POST /yaad/decay", s.handleDecay)
	mux.HandleFunc("POST /yaad/gc", s.handleGC)
	mux.HandleFunc("GET /yaad/events", s.SSE.ServeHTTP)
	mux.HandleFunc("GET /yaad/replay/{session_id}", s.handleReplay)
	ServeDashboard(mux)
	mux.HandleFunc("POST /yaad/export/json", s.handleExportJSON)
	mux.HandleFunc("POST /yaad/export/markdown", s.handleExportMarkdown)
	mux.HandleFunc("POST /yaad/export/obsidian", s.handleExportObsidian)
	mux.HandleFunc("POST /yaad/import/json", s.handleImportJSON)
	mux.HandleFunc("POST /yaad/team/share", s.handleTeamShare)
	mux.HandleFunc("GET /yaad/team/memories", s.handleTeamMemories)
	mux.HandleFunc("POST /yaad/skill/store", s.handleSkillStore)
	mux.HandleFunc("GET /yaad/skill/list", s.handleSkillList)
	mux.HandleFunc("GET /yaad/skill/{name}", s.handleSkillGet)
	mux.HandleFunc("POST /yaad/bench", s.handleBench)
	mux.HandleFunc("POST /yaad/compact", s.handleCompact)
	mux.HandleFunc("GET /yaad/mental-model", s.handleMentalModel)
	mux.HandleFunc("GET /yaad/profile", s.handleProfile)
}

func (s *RESTServer) handleRemember(w http.ResponseWriter, r *http.Request) {
	var in engine.RememberInput
	if err := decodeJSON(r, &in); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpErr(w, err, 413)
		} else {
			httpErr(w, err, 400)
		}
		return
	}
	if in.Type != "" && !engine.IsValidNodeType(in.Type) {
		httpErr(w, fmt.Errorf("invalid node type: %q", in.Type), 400)
		return
	}
	node, err := s.eng.Remember(r.Context(), in)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	// Broadcast to SSE clients
	s.SSE.Publish("memory.created", node)
	httpJSON(w, node, 201)
}

func (s *RESTServer) handleRecall(w http.ResponseWriter, r *http.Request) {
	var opts engine.RecallOpts
	if err := decodeJSON(r, &opts); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpErr(w, err, 413)
		} else {
			httpErr(w, err, 400)
		}
		return
	}
	if opts.Depth > maxGraphDepth {
		httpErr(w, fmt.Errorf("depth exceeds maximum of %d", maxGraphDepth), 400)
		return
	}
	result, err := s.eng.Recall(r.Context(), opts)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, result, 200)
}

func (s *RESTServer) handleContext(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	result, err := s.eng.Context(r.Context(), project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, result, 200)
}

func (s *RESTServer) handleLink(w http.ResponseWriter, r *http.Request) {
	var edge storage.Edge
	if err := decodeJSON(r, &edge); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpErr(w, err, 413)
		} else {
			httpErr(w, err, 400)
		}
		return
	}
	if edge.Type == "" {
		httpErr(w, fmt.Errorf("edge type is required"), 400)
		return
	}
	if !graph.IsValidEdgeType(edge.Type) {
		httpErr(w, fmt.Errorf("invalid edge type: %q", edge.Type), 400)
		return
	}
	if edge.ID == "" {
		edge.ID = uuid.New().String()
	}
	if err := s.eng.Graph().AddEdge(r.Context(), &edge); err != nil {
		httpErr(w, err, 400)
		return
	}
	httpJSON(w, edge, 201)
}

func (s *RESTServer) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.eng.Graph().RemoveEdge(r.Context(), id); err != nil {
		httpErr(w, err, 404)
		return
	}
	httpJSON(w, map[string]string{"status": "deleted"}, 200)
}

func (s *RESTServer) handleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, err := s.eng.Store().GetNode(r.Context(), id)
	if err != nil {
		httpErr(w, err, 404)
		return
	}
	neighbors, _ := s.eng.Store().GetNeighbors(r.Context(), id)
	httpJSON(w, map[string]any{"node": node, "neighbors": neighbors}, 200)
}

func (s *RESTServer) handleSubgraph(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	depth := intQuery(r, "depth", 2)
	if depth > maxGraphDepth {
		httpErr(w, fmt.Errorf("depth exceeds maximum of %d", maxGraphDepth), 400)
		return
	}
	sg, err := s.eng.Graph().ExtractSubgraph(r.Context(), id, depth)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, sg, 200)
}

func (s *RESTServer) handleImpact(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	depth := intQuery(r, "depth", 3)
	if depth > maxGraphDepth {
		httpErr(w, fmt.Errorf("depth exceeds maximum of %d", maxGraphDepth), 400)
		return
	}
	ids, err := s.eng.Graph().Impact(r.Context(), file, depth)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	var nodes []*storage.Node
	for _, id := range ids {
		if n, err := s.eng.Store().GetNode(r.Context(), id); err == nil {
			nodes = append(nodes, n)
		}
	}
	httpJSON(w, nodes, 200)
}

func (s *RESTServer) handleForget(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.eng.Forget(r.Context(), id); err != nil {
		httpErr(w, err, 404)
		return
	}
	httpJSON(w, map[string]string{"status": "forgotten"}, 200)
}

func (s *RESTServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Actually verify database connectivity with a lightweight query
	_, err := s.eng.Store().ListNodes(r.Context(), storage.NodeFilter{})
	if err != nil {
		httpJSON(w, map[string]string{"status": "error", "error": err.Error()}, 503)
		return
	}
	httpJSON(w, map[string]string{"status": "ok", "version": version.String()}, 200)
}

func (s *RESTServer) handleVersion(w http.ResponseWriter, _ *http.Request) {
	httpJSON(w, map[string]string{"version": version.String()}, 200)
}

func (s *RESTServer) handleStats(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	st, err := s.eng.Status(r.Context(), project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, st, 200)
}

func (s *RESTServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	limit := intQuery(r, "limit", 10)
	sessions, err := s.eng.Store().ListSessions(r.Context(), project, limit)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, sessions, 200)
}

func (s *RESTServer) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Project string `json:"project"`
		Agent   string `json:"agent"`
	}
	if err := decodeJSON(r, &body); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpErr(w, err, 413)
		} else {
			httpErr(w, err, 400)
		}
		return
	}
	sess := &storage.Session{
		ID:        uuid.New().String(),
		Project:   body.Project,
		Agent:     body.Agent,
		StartedAt: time.Now(),
	}
	if err := s.eng.Store().CreateSession(r.Context(), sess); err != nil {
		httpErr(w, err, 500)
		return
	}
	ctxRes, _ := s.eng.Context(r.Context(), body.Project)
	httpJSON(w, map[string]any{"session": sess, "context": ctxRes}, 201)
}

func (s *RESTServer) handleSessionEnd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	}
	if err := decodeJSON(r, &body); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpErr(w, err, 413)
		} else {
			httpErr(w, err, 400)
		}
		return
	}
	if err := s.eng.Store().EndSession(r.Context(), body.ID, body.Summary); err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]string{"status": "ended"}, 200)
}

func (s *RESTServer) handleStale(w http.ResponseWriter, _ *http.Request) {
	httpJSON(w, map[string]string{"status": "staleness detection available in Phase 2"}, 200)
}

func (s *RESTServer) handleEmbed(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID string `json:"node_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpErr(w, err, 413)
		} else {
			httpErr(w, err, 400)
		}
		return
	}
	if s.embedder == nil {
		httpErr(w, fmt.Errorf("no embedding provider configured"), 503)
		return
	}
	node, err := s.eng.Store().GetNode(r.Context(), body.NodeID)
	if err != nil {
		httpErr(w, err, 404)
		return
	}
	vec, err := s.embedder.Embed(r.Context(), node.Content)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	if err := s.eng.Store().SaveEmbedding(r.Context(), node.ID, s.embedder.Name(), vec); err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]any{"node_id": node.ID, "dims": len(vec)}, 200)
}

func (s *RESTServer) handleHybridRecall(w http.ResponseWriter, r *http.Request) {
	var opts engine.RecallOpts
	if err := decodeJSON(r, &opts); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpErr(w, err, 413)
		} else {
			httpErr(w, err, 400)
		}
		return
	}
	hs := engine.NewHybridSearch(s.eng.Store(), s.eng.Graph(), s.embedder)
	scored, err := hs.Search(r.Context(), opts.Query, opts)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	reranked := engine.Rerank(r.Context(), scored, s.eng.Store())
	httpJSON(w, reranked, 200)
}

func (s *RESTServer) handleProactive(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	hs := engine.NewHybridSearch(s.eng.Store(), s.eng.Graph(), s.embedder)
	pc := engine.NewProactiveContext(s.eng, hs)
	nodes, err := pc.Predict(r.Context(), project, 2000)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]any{
		"nodes":   nodes,
		"context": engine.FormatContext(nodes),
	}, 200)
}

func (s *RESTServer) handleFeedback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID         string               `json:"id"`
		Action     engine.FeedbackAction `json:"action"`
		NewContent string               `json:"new_content"`
	}
	if err := decodeJSON(r, &body); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpErr(w, err, 413)
		} else {
			httpErr(w, err, 400)
		}
		return
	}
	if err := s.eng.Feedback(r.Context(), body.ID, body.Action, body.NewContent); err != nil {
		httpErr(w, err, 400)
		return
	}
	httpJSON(w, map[string]string{"status": "ok"}, 200)
}

func (s *RESTServer) handleDecay(w http.ResponseWriter, r *http.Request) {
	if err := engine.RunDecay(r.Context(), s.eng.Store(), engine.DefaultDecayConfig); err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]string{"status": "decay applied"}, 200)
}

func (s *RESTServer) handleGC(w http.ResponseWriter, r *http.Request) {
	n, err := engine.GarbageCollect(r.Context(), s.eng.Store(), engine.DefaultDecayConfig)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]int{"removed": n}, 200)
}

func (s *RESTServer) handleReplay(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	events, err := s.eng.Store().GetReplayEvents(r.Context(), sessionID)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, events, 200)
}

func (s *RESTServer) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	data, err := exportimport.ExportJSON(r.Context(), s.eng.Store(), project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

func (s *RESTServer) handleExportMarkdown(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	md, err := exportimport.ExportMarkdown(r.Context(), s.eng.Store(), project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	w.Header().Set("Content-Type", "text/markdown")
	w.WriteHeader(200)
	fmt.Fprint(w, md)
}

func (s *RESTServer) handleExportObsidian(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Project  string `json:"project"`
		VaultDir string `json:"vault_dir"`
	}
	if err := decodeJSON(r, &body); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpErr(w, err, 413)
		} else {
			httpErr(w, err, 400)
		}
		return
	}
	n, err := exportimport.ExportObsidian(r.Context(), s.eng.Store(), body.Project, body.VaultDir)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]int{"written": n}, 200)
}

func (s *RESTServer) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		httpErr(w, err, 400)
		return
	}
	nodes, edges, err := exportimport.ImportJSON(r.Context(), s.eng.Store(), data)
	if err != nil {
		httpErr(w, err, 400)
		return
	}
	httpJSON(w, map[string]int{"nodes": nodes, "edges": edges}, 200)
}

func (s *RESTServer) handleTeamShare(w http.ResponseWriter, r *http.Request) {
	var body team.ShareInput
	if err := decodeJSON(r, &body); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpErr(w, err, 413)
		} else {
			httpErr(w, err, 400)
		}
		return
	}
	// For simplicity, share within the same store (global scope)
	node, err := team.Share(r.Context(), s.eng.Store(), s.eng.Store(), body)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, node, 201)
}

func (s *RESTServer) handleTeamMemories(w http.ResponseWriter, r *http.Request) {
	teamID := r.URL.Query().Get("team_id")
	nodes, err := team.ListTeamMemories(r.Context(), s.eng.Store(), teamID)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, nodes, 200)
}

func (s *RESTServer) handleSkillStore(w http.ResponseWriter, r *http.Request) {
	var sk skill.Skill
	if err := decodeJSON(r, &sk); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpErr(w, err, 413)
		} else {
			httpErr(w, err, 400)
		}
		return
	}
	project := r.URL.Query().Get("project")
	node, err := skill.Store(r.Context(), s.eng, &sk, project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, node, 201)
}

func (s *RESTServer) handleSkillList(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	skills, err := skill.ListSkills(r.Context(), s.eng.Store(), project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, skills, 200)
}

func (s *RESTServer) handleSkillGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	project := r.URL.Query().Get("project")
	sk, err := skill.Load(r.Context(), s.eng.Store(), name, project)
	if err != nil {
		httpErr(w, err, 404)
		return
	}
	httpJSON(w, map[string]string{"replay": skill.Replay(sk)}, 200)
}

func (s *RESTServer) handleBench(w http.ResponseWriter, r *http.Request) {
	result := bench.Run(r.Context(), s.eng, bench.DefaultQAs(), 2, 10)
	httpJSON(w, map[string]string{"report": result.String()}, 200)
}

func (s *RESTServer) handleCompact(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	n, err := s.eng.Compact(r.Context(), project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]int{"compacted": n}, 200)
}

func (s *RESTServer) handleMentalModel(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	model, err := s.eng.MentalModel(r.Context(), project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]any{"model": model, "formatted": model.Format()}, 200)
}

func (s *RESTServer) handleProfile(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	p, err := s.eng.Profile(r.Context(), project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]any{"profile": p, "formatted": p.Format()}, 200)
}

// --- helpers ---

func httpJSON(w http.ResponseWriter, v any, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, err error, code int) {
	httpJSON(w, map[string]string{"error": err.Error()}, code)
}

// decodeJSON unmarshals JSON from the request body, returning 413 if the body
// exceeds MaxBytesReader limits.
func decodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return fmt.Errorf("request body exceeds %d bytes: %w", maxBytesErr.Limit, err)
		}
		return err
	}
	return nil
}

func intQuery(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	var n int
	fmt.Sscanf(v, "%d", &n)
	if n <= 0 {
		return def
	}
	return n
}
