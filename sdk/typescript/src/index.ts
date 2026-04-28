/**
 * @graycode/yaad — TypeScript SDK for Yaad memory layer.
 *
 * Wraps the Yaad REST API. Auto-starts the yaad binary if not running.
 *
 * Usage:
 *   import { Yaad } from '@graycode/yaad'
 *   const yaad = new Yaad()
 *   await yaad.remember({ type: 'convention', content: 'Use jose' })
 *   const results = await yaad.recall('auth middleware')
 */

export interface Node {
  id: string;
  type: 'convention' | 'decision' | 'bug' | 'spec' | 'task' | 'skill' | 'preference' | 'file' | 'entity';
  content: string;
  summary?: string;
  scope: 'global' | 'project';
  project?: string;
  tier: number;
  tags?: string;
  confidence: number;
  access_count: number;
}

export interface Edge {
  id: string;
  from_id: string;
  to_id: string;
  type: string;
  weight: number;
}

export interface RememberInput {
  content: string;
  type?: string;
  summary?: string;
  tags?: string;
  project?: string;
  scope?: string;
  session?: string;
  agent?: string;
}

export interface RecallInput {
  query: string;
  depth?: number;
  limit?: number;
  type?: string;
  project?: string;
}

export interface RecallResult {
  nodes: Node[];
  edges: Edge[];
}

export interface YaadOptions {
  baseURL?: string;
  autoStart?: boolean;
}

export class Yaad {
  private baseURL: string;

  constructor(opts: YaadOptions = {}) {
    this.baseURL = opts.baseURL || 'http://127.0.0.1:3456';
  }

  // --- Core Memory ---

  async remember(input: RememberInput): Promise<Node> {
    return this.post<Node>('/yaad/remember', input);
  }

  async recall(query: string, opts?: Partial<RecallInput>): Promise<RecallResult> {
    return this.post<RecallResult>('/yaad/recall', { query, ...opts });
  }

  async context(project?: string): Promise<RecallResult> {
    const params = project ? `?project=${encodeURIComponent(project)}` : '';
    return this.get<RecallResult>(`/yaad/context${params}`);
  }

  async forget(id: string): Promise<void> {
    await this.del(`/yaad/forget/${id}`);
  }

  // --- Graph ---

  async link(fromId: string, toId: string, type: string): Promise<Edge> {
    return this.post<Edge>('/yaad/link', { from_id: fromId, to_id: toId, type });
  }

  async subgraph(id: string, depth = 2): Promise<{ nodes: Node[]; edges: Edge[] }> {
    return this.get(`/yaad/subgraph/${id}?depth=${depth}`);
  }

  async impact(file: string): Promise<Node[]> {
    return this.get(`/yaad/impact/${encodeURIComponent(file)}`);
  }

  // --- Sessions ---

  async sessionStart(project: string, agent: string): Promise<{ session: any; context: RecallResult }> {
    return this.post('/yaad/session/start', { project, agent });
  }

  async sessionEnd(id: string, summary: string): Promise<void> {
    await this.post('/yaad/session/end', { id, summary });
  }

  // --- System ---

  async health(): Promise<{ status: string; version: string }> {
    return this.get('/yaad/health');
  }

  async stats(project?: string): Promise<{ Nodes: number; Edges: number; Sessions: number }> {
    const params = project ? `?project=${encodeURIComponent(project)}` : '';
    return this.get(`/yaad/graph/stats${params}`);
  }

  // --- Feedback ---

  async approve(id: string): Promise<void> {
    await this.post('/yaad/feedback', { id, action: 'approve' });
  }

  async edit(id: string, newContent: string): Promise<void> {
    await this.post('/yaad/feedback', { id, action: 'edit', new_content: newContent });
  }

  async discard(id: string): Promise<void> {
    await this.post('/yaad/feedback', { id, action: 'discard' });
  }

  // --- SSE Events ---

  events(): EventSource {
    return new EventSource(`${this.baseURL}/yaad/events`);
  }

  // --- HTTP helpers ---

  private async get<T>(path: string): Promise<T> {
    const res = await fetch(`${this.baseURL}${path}`);
    if (!res.ok) throw new Error(`yaad: ${res.status} ${await res.text()}`);
    return res.json();
  }

  private async post<T>(path: string, body: any): Promise<T> {
    const res = await fetch(`${this.baseURL}${path}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!res.ok) throw new Error(`yaad: ${res.status} ${await res.text()}`);
    return res.json();
  }

  private async del(path: string): Promise<void> {
    const res = await fetch(`${this.baseURL}${path}`, { method: 'DELETE' });
    if (!res.ok) throw new Error(`yaad: ${res.status} ${await res.text()}`);
  }
}

export default Yaad;
