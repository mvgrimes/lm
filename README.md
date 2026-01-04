# Link Manager

A terminal-based tool to manage links, tasks, knowledge, and reading lists.

## Features

- **Add Links**: Save URLs with automatic content fetching and AI-powered summarization
- **Tasks**: Organize links by task and open all related links at once
- **Read Later**: Curated list of links with summaries for later reading
- **Remember**: Categorize and tag links for long-term reference
- **Search**: Full-text search across all saved links

## Technology Stack

- **Language**: Go 1.24
- **Database**: SQLite3 (pure Go, no CGO required via modernc.org/sqlite)
- **TUI Framework**: Charm (Bubbletea, Lipgloss, Bubbles)
- **Database Queries**: sqlc for type-safe SQL
- **Migrations**: goose for database migrations
- **AI Summarization**: OpenAI API (optional)

## Installation

```bash
# Clone the repository
git clone <your-repo-url>
cd links

# Build the application
go build -o lk main.go

# Or use make/just
make build
# or
just build
```

## Configuration

Create a `.env` file in the project root (or copy `.env.example`):

```bash
# OpenAI API Key for link summarization (optional)
OPENAI_API_KEY=your_api_key_here

# Database path (optional, defaults to ~/.lk.db)
DB_PATH=/path/to/your/database.db

# Mode (production or development)
MODE=development
```

## Usage

Simply run the application to start the TUI:

```bash
./lk
```

### Navigation

- Use **arrow keys** or **j/k** to navigate menus and lists
- Press **Enter** to select an item or confirm an action
- Press **q** to go back to the previous screen
- Press **Ctrl+C** to quit the application

### Modes

1. **Add Link**: Enter a URL to fetch, extract text, and optionally generate an AI summary
2. **Tasks**: View tasks and their associated links. Press **o** to open all links for a task
3. **Read Later**: Browse links saved for later reading with summaries
4. **Remember**: View links for categorization (tagging feature coming soon)
5. **Search**: Search across all link content, titles, and summaries

## Database

The application uses SQLite3 with the following schema:
- **links**: URLs with titles, content, summaries, and status
- **tasks**: Task definitions
- **categories**: Organization categories
- **tags**: Tagging system
- **Junction tables**: Many-to-many relationships between links, tasks, categories, and tags

Migrations are automatically applied on startup using embedded migration files.

## Development

### Generate SQL queries

After modifying `internal/database/queries.sql`:

```bash
sqlc generate
# or
make generate
```

### Adding migrations

Create a new migration file in `internal/database/migrations/`:

```sql
-- +goose Up
-- Your migration SQL here

-- +goose Down
-- Rollback SQL here
```

## Project Structure

```
.
├── cmd/                    # Cobra commands
│   └── root.go            # Main command and TUI launcher
├── internal/
│   ├── database/          # Database layer
│   │   ├── database.go   # Database connection and migrations
│   │   ├── migrations/   # SQL migration files
│   │   └── queries.sql   # SQL queries for sqlc
│   ├── models/           # Generated models from sqlc
│   ├── services/         # Business logic services
│   │   ├── fetcher.go   # HTTP content fetching
│   │   ├── extractor.go # HTML text extraction
│   │   └── summarizer.go# OpenAI summarization
│   └── tui/             # Terminal UI components
│       ├── model.go     # Main TUI model
│       ├── menu.go      # Main menu
│       ├── addlink.go   # Add link screen
│       ├── tasks.go     # Tasks screen
│       ├── readlater.go # Read later screen
│       ├── remember.go  # Remember screen
│       └── search.go    # Search screen
├── main.go              # Application entry point
├── go.mod              # Go dependencies
├── sqlc.yaml           # sqlc configuration
├── .env.example        # Example environment variables
└── README.md          # This file
```

## License

[Your License Here]

