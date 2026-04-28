package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	stdtls "crypto/tls"
	"time"

	"github.com/google/uuid"
	"github.com/yaadmemory/yaad/internal/bench"
	"github.com/yaadmemory/yaad/internal/bridge"
	"github.com/yaadmemory/yaad/internal/embeddings"
	"github.com/yaadmemory/yaad/internal/engine"
	"github.com/yaadmemory/yaad/internal/exportimport"
	"github.com/yaadmemory/yaad/internal/skill"
	"github.com/yaadmemory/yaad/internal/storage"
	"github.com/yaadmemory/yaad/internal/team"
)

// RESTServer serves the HTTP API.
type RESTServer struct {
	eng      *engine.Engine
	addr     string
	tlsCfg   *stdtls.Config
	embedder embeddings.Provider // nil = no vector search
	SSE      *SSEBroker
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

// ListenAndServe starts the HTTP server.
func (s *RESTServer) ListenAndServe() error {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := &http.Server{Addr: s.addr, Handler: mux, TLSConfig: s.tlsCfg}
	if s.tlsCfg != nil {
		fmt.Printf("yaad REST API (HTTPS) listening on %s\n", s.addr)
		ln, err := stdtls.Listen("tcp", s.addr, s.tlsCfg)
		if err != nil {
			return err
		}
		return srv.Serve(ln)
	}
	fmt.Printf("yaad REST API listening on %s\n", s.addr)
	return srv.ListenAndServe()
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
	mux.HandleFunc("POST /yaad/bridge/import", s.handleBridgeImport)
	mux.HandleFunc("POST /yaad/bridge/export", s.handleBridgeExport)
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
}

func (s *RESTServer) handleRemember(w http.ResponseWriter, r *http.Request) {
	var in engine.RememberInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpErr(w, err, 400)
		return
	}
	node, err := s.eng.Remember(in)
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
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		httpErr(w, err, 400)
		return
	}
	result, err := s.eng.Recall(opts)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, result, 200)
}

func (s *RESTServer) handleContext(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	result, err := s.eng.Context(project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, result, 200)
}

func (s *RESTServer) handleLink(w http.ResponseWriter, r *http.Request) {
	var edge storage.Edge
	if err := json.NewDecoder(r.Body).Decode(&edge); err != nil {
		httpErr(w, err, 400)
		return
	}
	if edge.ID == "" {
		edge.ID = uuid.New().String()
	}
	if err := s.eng.Graph().AddEdge(&edge); err != nil {
		httpErr(w, err, 400)
		return
	}
	httpJSON(w, edge, 201)
}

func (s *RESTServer) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.eng.Graph().RemoveEdge(id); err != nil {
		httpErr(w, err, 404)
		return
	}
	httpJSON(w, map[string]string{"status": "deleted"}, 200)
}

func (s *RESTServer) handleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, err := s.eng.Store().GetNode(id)
	if err != nil {
		httpErr(w, err, 404)
		return
	}
	neighbors, _ := s.eng.Store().GetNeighbors(id)
	httpJSON(w, map[string]any{"node": node, "neighbors": neighbors}, 200)
}

func (s *RESTServer) handleSubgraph(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	depth := intQuery(r, "depth", 2)
	sg, err := s.eng.Graph().ExtractSubgraph(id, depth)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, sg, 200)
}

func (s *RESTServer) handleImpact(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	depth := intQuery(r, "depth", 3)
	ids, err := s.eng.Graph().Impact(file, depth)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	var nodes []*storage.Node
	for _, id := range ids {
		if n, err := s.eng.Store().GetNode(id); err == nil {
			nodes = append(nodes, n)
		}
	}
	httpJSON(w, nodes, 200)
}

func (s *RESTServer) handleForget(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.eng.Forget(id); err != nil {
		httpErr(w, err, 404)
		return
	}
	httpJSON(w, map[string]string{"status": "forgotten"}, 200)
}

func (s *RESTServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	httpJSON(w, map[string]string{"status": "ok", "version": "0.1.0"}, 200)
}

func (s *RESTServer) handleStats(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	st, err := s.eng.Status(project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, st, 200)
}

func (s *RESTServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	limit := intQuery(r, "limit", 10)
	sessions, err := s.eng.Store().ListSessions(project, limit)
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
	_ = json.NewDecoder(r.Body).Decode(&body)
	sess := &storage.Session{
		ID:        uuid.New().String(),
		Project:   body.Project,
		Agent:     body.Agent,
		StartedAt: time.Now(),
	}
	if err := s.eng.Store().CreateSession(sess); err != nil {
		httpErr(w, err, 500)
		return
	}
	ctx, _ := s.eng.Context(body.Project)
	httpJSON(w, map[string]any{"session": sess, "context": ctx}, 201)
}

func (s *RESTServer) handleSessionEnd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpErr(w, err, 400)
		return
	}
	if err := s.eng.Store().EndSession(body.ID, body.Summary); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpErr(w, err, 400)
		return
	}
	if s.embedder == nil {
		httpErr(w, fmt.Errorf("no embedding provider configured"), 503)
		return
	}
	node, err := s.eng.Store().GetNode(body.NodeID)
	if err != nil {
		httpErr(w, err, 404)
		return
	}
	vec, err := s.embedder.Embed(r.Context(), node.Content)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	if err := s.eng.Store().SaveEmbedding(node.ID, s.embedder.Name(), vec); err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]any{"node_id": node.ID, "dims": len(vec)}, 200)
}

func (s *RESTServer) handleHybridRecall(w http.ResponseWriter, r *http.Request) {
	var opts engine.RecallOpts
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		httpErr(w, err, 400)
		return
	}
	hs := engine.NewHybridSearch(s.eng.Store(), s.eng.Graph(), s.embedder)
	scored, err := hs.Search(r.Context(), opts.Query, opts)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	reranked := engine.Rerank(scored, s.eng.Store())
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpErr(w, err, 400)
		return
	}
	if err := s.eng.Feedback(body.ID, body.Action, body.NewContent); err != nil {
		httpErr(w, err, 400)
		return
	}
	httpJSON(w, map[string]string{"status": "ok"}, 200)
}

func (s *RESTServer) handleDecay(w http.ResponseWriter, _ *http.Request) {
	if err := engine.RunDecay(s.eng.Store(), engine.DefaultDecayConfig); err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]string{"status": "decay applied"}, 200)
}

func (s *RESTServer) handleGC(w http.ResponseWriter, _ *http.Request) {
	n, err := engine.GarbageCollect(s.eng.Store(), engine.DefaultDecayConfig)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]int{"removed": n}, 200)
}

func (s *RESTServer) handleBridgeImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Dir     string `json:"dir"`
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpErr(w, err, 400)
		return
	}
	n, err := bridge.Import(s.eng, body.Dir, body.Project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]int{"imported": n}, 200)
}

func (s *RESTServer) handleBridgeExport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Dir     string `json:"dir"`
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpErr(w, err, 400)
		return
	}
	if err := bridge.Export(s.eng.Store(), body.Dir, body.Project); err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, map[string]string{"status": "exported"}, 200)
}

func (s *RESTServer) handleReplay(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	events, err := s.eng.Store().GetReplayEvents(sessionID)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, events, 200)
}

func (s *RESTServer) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	data, err := exportimport.ExportJSON(s.eng.Store(), project)
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
	md, err := exportimport.ExportMarkdown(s.eng.Store(), project)
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpErr(w, err, 400)
		return
	}
	n, err := exportimport.ExportObsidian(s.eng.Store(), body.Project, body.VaultDir)
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
	nodes, edges, err := exportimport.ImportJSON(s.eng.Store(), data)
	if err != nil {
		httpErr(w, err, 400)
		return
	}
	httpJSON(w, map[string]int{"nodes": nodes, "edges": edges}, 200)
}

func (s *RESTServer) handleTeamShare(w http.ResponseWriter, r *http.Request) {
	var body team.ShareInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpErr(w, err, 400)
		return
	}
	// For simplicity, share within the same store (global scope)
	node, err := team.Share(s.eng.Store(), s.eng.Store(), body)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, node, 201)
}

func (s *RESTServer) handleTeamMemories(w http.ResponseWriter, r *http.Request) {
	teamID := r.URL.Query().Get("team_id")
	nodes, err := team.ListTeamMemories(s.eng.Store(), teamID)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, nodes, 200)
}

func (s *RESTServer) handleSkillStore(w http.ResponseWriter, r *http.Request) {
	var sk skill.Skill
	if err := json.NewDecoder(r.Body).Decode(&sk); err != nil {
		httpErr(w, err, 400)
		return
	}
	project := r.URL.Query().Get("project")
	node, err := skill.Store(s.eng, &sk, project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, node, 201)
}

func (s *RESTServer) handleSkillList(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	skills, err := skill.ListSkills(s.eng.Store(), project)
	if err != nil {
		httpErr(w, err, 500)
		return
	}
	httpJSON(w, skills, 200)
}

func (s *RESTServer) handleSkillGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	project := r.URL.Query().Get("project")
	sk, err := skill.Load(s.eng.Store(), name, project)
	if err != nil {
		httpErr(w, err, 404)
		return
	}
	httpJSON(w, map[string]string{"replay": skill.Replay(sk)}, 200)
}

func (s *RESTServer) handleBench(w http.ResponseWriter, r *http.Request) {
	result := bench.Run(s.eng, bench.DefaultQAs(), 2, 10)
	httpJSON(w, map[string]string{"report": result.String()}, 200)
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
