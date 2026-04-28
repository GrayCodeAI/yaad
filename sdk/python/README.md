# yaad — Python SDK

Persistent memory for coding agents. Zero dependencies.

```bash
pip install yaad
```

```python
from yaad import Yaad

y = Yaad()  # connects to yaad server on localhost:3456

# Store a memory
y.remember(content="Use jose for JWT", type="convention")

# Search memories (graph-aware)
results = y.recall("auth middleware")
for node in results.nodes:
    print(f"[{node.type}] {node.content}")

# Get session context
ctx = y.context()

# Impact analysis
affected = y.impact("src/auth.ts")

# Mental model
model = y.mental_model()
```

Requires the `yaad` binary running: `yaad serve`

Docs: https://github.com/GrayCodeAI/yaad
