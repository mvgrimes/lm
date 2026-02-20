# lm · Link Manager

A terminal-based tool to manage links, tasks, activities, and reading lists.

## Features

- **Links** — Save URLs with automatic content fetching and AI-powered summarization
- **Tasks** — Organize completable work items and open all related links at once
- **Activities** — Track ongoing, non-completable activities with associated links
- **Read Later** — Curated list of links with summaries for later reading
- **Tags** — Create and apply tags; browse links by tag
- **Categories** — Organize links into categories; browse links by category
- **Search** — Full-text search across all saved link titles, content, and summaries

## Technology Stack

- **Language**: Go 1.24
- **Database**: SQLite3 (pure Go, no CGO via `modernc.org/sqlite`)
- **TUI Framework**: Charm (Bubbletea, Lipgloss, Bubbles)
- **Database Queries**: sqlc for type-safe SQL
- **Migrations**: goose (run automatically on startup)
- **AI Summarization**: OpenAI GPT-4o-mini (optional)
- **HTML Parsing**: goquery

## Installation

```bash
# Clone the repository
git clone <your-repo-url>
cd lm

# Build
go build -o lm main.go
# or
make build
```

## Configuration

Config files live in `~/.config/lm/`. Create `~/.config/lm/.env`:

```bash
# OpenAI API key — optional, enables summarization and tag/category suggestions
OPENAI_API_KEY=your_api_key_here

# Database path — optional, defaults to ~/.config/lm/lm.db
DB_PATH=/path/to/your/database.db

# Logging mode — "production" uses JSON, anything else uses colored text
MODE=development
```

The config directory and database are created automatically on first run.

## Usage

```bash
./lm          # start
./lm -d       # start with debug logging
```

The application requires an interactive terminal (TTY).

### Navigation

| Key | Action |
|-----|--------|
| `Ctrl+N` / `Ctrl+P` | Next / previous tab |
| `Ctrl+A` | Open Add Link modal (any tab) |
| `Ctrl+C` | Quit |
| `↑` / `↓` or `k` / `j` | Navigate lists |
| `Enter` | Select / confirm |
| `PgUp` / `PgDn` | Scroll detail views |
| `Esc` | Close modal / cancel |

### Tabs

#### Links
Split-view layout (35% list · 65% detail). Press `/` to search. Detail panel shows title, URL, summary, tags, categories, and full page content.

#### Tasks
Completable work items with associated links.

| Key | Action |
|-----|--------|
| `n` | Create new task |
| `a` | Add link to selected task |
| `c` | Toggle task completion |
| `o` | Open all task links in browser |

#### Activities
Ongoing, non-completable activities with associated links. Same interface as Tasks minus the completion toggle.

| Key | Action |
|-----|--------|
| `n` | Create new activity |
| `a` | Add link to selected activity |
| `o` | Open all activity links in browser |

#### Read Later
Split-view of links with `status = read_later`. All newly added links land here by default.

#### Tags / Categories
Create and manage tags or categories. Press `n` to create, `Enter` to view associated links, `d` to delete.

---

## Architecture

### Link Ingestion Pipeline

When a URL is submitted via the Add Link modal (`Ctrl+A`), the following steps run in a background goroutine so the UI stays responsive:

```
 User submits URL
        │
        ▼
┌───────────────┐
│   Duplicate   │  URL already in DB?
│    Check      │──────────────────────► Return existing record
└───────┬───────┘
        │ new URL
        ▼
┌───────────────┐
│    Fetcher    │  HTTP GET with browser-like headers
│  FetchURL()   │  Retries once on HTTP 202 Accepted (750 ms delay)
└───────┬───────┘
        │ raw HTML
        ▼
┌───────────────┐
│   Extractor   │  goquery parses HTML
│ ExtractText() │  • Strips <script>, <style>, <nav>, <header>, <footer>
│               │  • Extracts from <article>/<main>/.content first
│               │  • Falls back to <p>, <h1-6>, <li> elements
│               │  • Returns: title (from <title>), cleaned text
└───────┬───────┘
        │ title + text
        ▼
┌───────────────────────────────┐
│         Summarizer            │  OpenAI GPT-4o-mini (optional)
│  Summarize()                  │  → 2–3 sentence summary (≤200 tokens)
│  SuggestMetadata()            │  → suggested category + 3–5 tags
└───────────────┬───────────────┘
                │ summary, category, tags
                ▼
┌───────────────────────────────┐
│         SQLite (sqlc)         │
│  CreateLink()                 │  Stores url, title, content (≤10 k chars),
│                               │  summary, status = "read_later"
└───────────────┬───────────────┘
                │ INSERT trigger fires
                ▼
┌───────────────────────────────┐
│       FTS5 Virtual Table      │  links_fts automatically indexed
│       (links_fts)             │  via INSERT/UPDATE/DELETE triggers
└───────────────────────────────┘
                │
                ▼
       linkProcessCompleteMsg
       sent back to UI thread
                │
                ▼
┌───────────────────────────────┐
│          Add Link Modal       │  Shows summary preview
│                               │  Auto-fills suggested category & tags
│                               │  User can edit and Save metadata
└───────────────────────────────┘
```

### Metadata Save (separate step)

After the link is stored the user can confirm or edit the AI-suggested category and tags, then press **Save**:

```
User edits category / tags → Save
        │
        ├─► GetCategoryByName → create if missing → LinkCategory
        │
        └─► GetTagByName (per tag) → create if missing → LinkTag
```

### Data Model

```
links ──┬── link_tasks      ──── tasks
        ├── link_activities  ──── activities
        ├── link_tags        ──── tags
        └── link_categories  ──── categories

links_fts  (FTS5 virtual table, auto-synced via triggers)
```

### TUI Architecture

The application follows the Bubbletea Elm architecture (Model → Update → View):

```
main.go
  └─ cmd/root.go          CLI setup, DB init, program launch
       └─ tui/model.go    Root model — tab routing, modal overlay
            ├─ links.go         Links tab
            ├─ tasks.go         Tasks tab
            ├─ activities.go    Activities tab
            ├─ readlater.go     Read Later tab
            ├─ tags.go          Tags tab
            ├─ categories.go    Categories tab
            └─ addlink.go       Add Link modal (shared by tabs)
```

---

## Development

### Generate SQL query code

After modifying `internal/database/queries.sql`:

```bash
sqlc generate
# or
make generate
```

### Add a migration

Create a numbered file in `internal/database/migrations/`:

```sql
-- +goose Up
ALTER TABLE links ADD COLUMN my_field TEXT;

-- +goose Down
-- SQLite doesn't support DROP COLUMN easily; handle as needed
```

Migrations run automatically on startup via embedded files.

## Project Structure

```
.
├── cmd/
│   └── root.go                 # CLI setup and TUI launcher
├── internal/
│   ├── database/
│   │   ├── database.go         # Connection and migration runner
│   │   ├── migrations/         # goose SQL migration files
│   │   └── queries.sql         # sqlc source queries
│   ├── models/                 # sqlc-generated types and query methods
│   ├── services/
│   │   ├── fetcher.go          # HTTP content fetching
│   │   ├── extractor.go        # HTML → plain text extraction
│   │   └── summarizer.go       # OpenAI summarization and metadata suggestions
│   └── tui/
│       ├── model.go            # Root model, tab switching, modal overlay
│       ├── addlink.go          # Add Link form (standalone and modal)
│       ├── links.go            # Links tab
│       ├── tasks.go            # Tasks tab
│       ├── activities.go       # Activities tab
│       ├── readlater.go        # Read Later tab
│       ├── tags.go             # Tags tab
│       └── categories.go       # Categories tab
├── main.go
├── go.mod
├── sqlc.yaml
└── .env.example
```

## License

[Your License Here]
