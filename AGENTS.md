# AI.md - Link Manager Project Summary

## Project Overview

**Link Manager** (`lm`) is a terminal-based application for managing links, tasks, knowledge, and reading lists. It provides a rich TUI (Terminal User Interface) for organizing web content with AI-powered summarization capabilities.

**Version**: 1.0.0
**Language**: Go 1.24
**Module**: `mccwk.com/lm`

## Core Functionality

### Features
1. **Links Tab** - Browse and search all links with integrated search bar and split-view detail panel
2. **Tasks Tab** - Organize links by task; create tasks, add links to tasks, open all task links at once
3. **Read Later Tab** - Curated list of links marked for later reading with split-view details
4. **Tags Tab** - Create and manage tags, view links associated with each tag
5. **Categories Tab** - Create and manage categories, view links in each category
6. **Add Link Modal** - Accessible from any tab via Ctrl+A for quick link addition with AI summarization

## Architecture

### Technology Stack
- **Language**: Go 1.24
- **Database**: SQLite3 (pure Go via `modernc.org/sqlite`, no CGO required)
- **TUI Framework**: Charm ecosystem (Bubbletea, Lipgloss, Bubbles)
- **Database Queries**: sqlc for type-safe SQL generation
- **Migrations**: goose for database schema migrations
- **AI Summarization**: OpenAI API (optional)
- **HTML Parsing**: goquery for content extraction
- **Logging**: slog with tint for colored console output
- This project uses a CLI ticket system for task management. Run `tk help` when you need to use it.

### Project Structure
```
.
├── cmd/                         # Cobra commands
│   └── root.go                 # Main command and TUI launcher (105 lines)
├── internal/
│   ├── database/               # Database layer
│   │   ├── database.go        # Database connection and migrations
│   │   ├── migrations/        # SQL migration files
│   │   │   └── 001_initial_schema.sql
│   │   └── queries.sql        # SQL queries for sqlc (195 lines)
│   ├── models/                # Generated models from sqlc
│   │   ├── db.go             # Database interface
│   │   ├── models.go         # Type definitions
│   │   └── queries.sql.go    # Generated query code
│   ├── services/             # Business logic services
│   │   ├── fetcher.go       # HTTP content fetching
│   │   ├── extractor.go     # HTML text extraction
│   │   └── summarizer.go    # OpenAI summarization
│   └── tui/                 # Terminal UI components
│       ├── model.go         # Main TUI model with tab navigation
│       ├── addlink.go       # Add link modal/dialog
│       ├── links.go         # Links tab with search and detail view
│       ├── tasks.go         # Tasks tab
│       ├── readlater.go     # Read later tab with detail view
│       ├── tags.go          # Tags tab
│       └── categories.go    # Categories tab
├── main.go                  # Application entry point (10 lines)
├── go.mod                   # Go dependencies
├── sqlc.yaml                # sqlc configuration
├── .env.example             # Example environment variables
└── README.md                # User documentation
```

**Total Go Code**: ~3,274 lines across all internal packages

## Database Schema

### Core Tables
1. **links** - Stores URLs with metadata
   - Fields: id, url (unique), title, content, summary, status, timestamps (created_at, updated_at, fetched_at, summarized_at)
   - Status values: `read_later`, `remember`, `archived`

2. **tasks** - Task definitions
   - Fields: id, name, description, completed (boolean), timestamps

3. **categories** - Organization categories
   - Fields: id, name (unique), description, created_at

4. **tags** - Tagging system
   - Fields: id, name (unique), created_at

### Junction Tables (Many-to-Many)
- **link_tasks** - Links ↔ Tasks
- **link_categories** - Links ↔ Categories
- **link_tags** - Links ↔ Tags

### Full-Text Search
- **links_fts** - FTS5 virtual table for full-text search on links
- Automatically synced with links table via triggers (insert, update, delete)

### Indexes
- `idx_links_status` - Query by status
- `idx_links_created_at` - Sort by creation date
- `idx_tasks_completed` - Filter completed tasks
- Junction table indexes for efficient joins

## Key Components

### Entry Point
- **main.go**: Minimal entry point that calls `cmd.Execute()`
- **cmd/root.go**:
  - Cobra command setup with `-d/--debug` flag
  - Environment variable loading via godotenv
  - Database initialization
  - TUI launch with Bubbletea

### Database Layer
- **database.go**:
  - Database connection management
  - Automatic migrations on startup using embedded migration files
  - Uses `modernc.org/sqlite` (pure Go, no CGO)

- **queries.sql**:
  - Type-safe SQL queries via sqlc
  - CRUD operations for all entities
  - Relationship management (linking/unlinking)
  - Full-text search queries

### Services Layer
1. **fetcher.go** - HTTP client for fetching web content
2. **extractor.go** - HTML parsing and text extraction using goquery
3. **summarizer.go** - OpenAI API integration for content summarization

### TUI Layer
- Built with Charm's Bubbletea (Elm architecture)
- **Tab-based navigation** with 5 main tabs:
  - **Links** - All links with integrated search (press `/` to search) and split-view details
  - **Tasks** - Task management with link associations
  - **Read Later** - Links marked for later reading with split-view details
  - **Tags** - Tag management and viewing links by tag
  - **Categories** - Category management and viewing links by category
- **Add Link Modal** - Accessible from any tab via Ctrl+A
- **Keyboard Controls**:
  - `Ctrl+[` / `Ctrl+]` - Navigate between tabs
  - `Ctrl+A` - Open add link modal (from anywhere)
  - `Ctrl+C` - Quit application
  - `/` - Focus search (in Links tab)
  - `arrows/j/k` - Navigate lists
  - `Enter/o` - Open selected link
  - `PgUp/PgDn` - Scroll detail views
  - `Esc` - Close modal or cancel search focus

## Configuration

### Environment Variables (~/.config/lm/.env)
```bash
OPENAI_API_KEY=your_api_key_here        # Optional, for AI summarization
DB_PATH=/path/to/database.db            # Optional, defaults to ~/.config/lm/lm.db
MODE=development                         # or "production" for JSON logging
```

### Config and Database Location
- Config directory: `~/.config/lm/`
- Default database: `~/.config/lm/lm.db`
- Environment file: `~/.config/lm/.env`
- Both configurable: `DB_PATH` env var overrides the database path

## Build and Run

### Commands
```bash
# Build
go build -o lm main.go
make build
just build

# Run
./lm

# Debug mode
./lm -d
```

### Requirements
- Go 1.24+
- No external C dependencies (pure Go SQLite)
- Interactive terminal (TTY required)

## Development Workflow

### Generating SQL Code
After modifying `internal/database/queries.sql`:
```bash
sqlc generate
# or
make generate
```

### Adding Migrations
Create a new file in `internal/database/migrations/`:
```sql
-- +goose Up
-- Your migration SQL here

-- +goose Down
-- Rollback SQL here
```

Migrations run automatically on application startup.

## Key Dependencies

### Core
- `github.com/charmbracelet/bubbletea` - TUI framework (Elm architecture)
- `github.com/charmbracelet/lipgloss` - Terminal styling
- `github.com/charmbracelet/bubbles` - TUI components
- `modernc.org/sqlite` - Pure Go SQLite driver
- `github.com/spf13/cobra` - CLI framework

### Database
- `github.com/pressly/goose/v3` - Database migrations
- sqlc - Type-safe SQL code generation (dev tool)

### Utilities
- `github.com/PuerkitoBio/goquery` - HTML parsing
- `github.com/sashabaranov/go-openai` - OpenAI API client
- `github.com/joho/godotenv` - .env file loading
- `github.com/pkg/browser` - Browser launching
- `github.com/lmittmann/tint` - Colored slog handler

## Navigation and UX

### Interface Design
The application uses a **tab-based interface** with tabs displayed across the top:
- `[ Links ]  Tasks  Read Later  Tags  Categories`
- Active tab is highlighted with bold text and colored border
- Title bar shows "lm · Link Manager"
- Tab content fills the rest of the screen

### Tab-Specific Features

#### Links Tab
- **Split-view layout**: Link list (left 35%) | Details (right 65%)
- Search bar at top of list panel (press `/` to focus)
- Real-time filtering as you type
- Detail view shows: title, URL, summary, tags, categories, full content
- Scrollable detail view (PgUp/PgDn)

#### Tasks Tab
- Create tasks with `n` key
- Add links to tasks with `a` key (opens add link modal)
- View task links with Enter
- Open all task links in browser with `o` key
- Toggle task completion with `c` key

#### Read Later Tab
- **Split-view layout**: Link list (left 35%) | Details (right 65%)
- Shows only links with status="read_later"
- Detail view with scrollable content

#### Tags Tab
- Create tags with `n` key
- View links for selected tag with Enter
- Delete tags with `d` key

#### Categories Tab
- Create categories with `n` key (includes optional description)
- View links for selected category with Enter
- Delete categories with `d` key

### Global Keyboard Controls
- `Ctrl+[` - Previous tab
- `Ctrl+]` - Next tab
- `Ctrl+A` - Open add link modal (works from any tab)
- `Ctrl+C` - Quit application
- `arrows/j/k` - Navigate lists
- `Enter` or `o` - Open/select item
- `PgUp/PgDn` - Scroll detail views
- `Esc` - Close modal or unfocus search
- `/` - Focus search (Links tab only)

## Current Status

### Implemented ✅
- Core database schema with FTS5 search
- Complete CRUD operations for all entities
- Web content fetching and extraction
- OpenAI summarization integration
- **Tab-based TUI with 5 main tabs**
- **Links tab with integrated search and detail view**
- **Tasks tab with link associations**
- **Read Later tab with detail view**
- **Tags tab with full CRUD operations**
- **Categories tab with full CRUD operations**
- **Add Link modal accessible via Ctrl+A from anywhere**
- Auto-migrations on startup
- Split-view layouts for Links and Read Later tabs

### Roadmap/TODO
- Link editing/updating via TUI (currently view-only in detail panels)
- Ability to add/remove tags and categories to links from the UI
- Link status management (move between read_later/remember/archived)
- Export functionality (JSON, CSV, HTML)
- Browser integration/extensions
- Keyboard shortcuts help screen
- Link deduplication on import

## Notes for AI Assistants

1. **Database**: Uses sqlc for type-safe queries. Always regenerate after modifying `queries.sql`
2. **Migrations**: Add new .sql files to `internal/database/migrations/` with goose format
3. **TUI Architecture**:
   - Bubbletea follows Elm architecture (Model-Update-View)
   - Main model in `model.go` handles tab switching and modal display
   - Each tab is a separate component: `links.go`, `tasks.go`, `readlater.go`, `tags.go`, `categories.go`
   - Add link functionality is in `addlink.go` and can be shown as a modal overlay
   - Tab navigation via `Ctrl+[` and `Ctrl+]`
   - Modal shown via `Ctrl+A` from any tab
4. **Logging**: Uses slog. Debug level by default in development, configurable via `-d` flag
5. **No CGO**: Project uses pure Go SQLite implementation - no C compiler needed
6. **Git**: Repository is already initialized (`.git` directory present)
7. **Binary**: `lm` binary is built and present in project root
8. **Code Style**: Standard Go conventions, uses early returns and error handling
9. **Layout Pattern**: Links and Read Later tabs use split-view (35% list, 65% detail) with viewport for scrolling
10. **Color Scheme**: Uses ANSI colors - 6=cyan (titles), 10=green (selected), 243=dim gray (secondary), 12=blue (links), etc.
