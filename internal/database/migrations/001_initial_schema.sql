-- +goose Up
-- Create links table
CREATE TABLE links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL UNIQUE,
    title TEXT,
    content TEXT,
    summary TEXT,
    status TEXT NOT NULL DEFAULT 'read_later', -- read_later, remember, archived
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    fetched_at DATETIME,
    summarized_at DATETIME
);

-- Create tasks table
CREATE TABLE tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    completed BOOLEAN NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create categories table
CREATE TABLE categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create tags table
CREATE TABLE tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create link_tasks junction table (many-to-many)
CREATE TABLE link_tasks (
    link_id INTEGER NOT NULL,
    task_id INTEGER NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (link_id, task_id),
    FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE CASCADE,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

-- Create link_categories junction table (many-to-many)
CREATE TABLE link_categories (
    link_id INTEGER NOT NULL,
    category_id INTEGER NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (link_id, category_id),
    FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE CASCADE,
    FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE CASCADE
);

-- Create link_tags junction table (many-to-many)
CREATE TABLE link_tags (
    link_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (link_id, tag_id),
    FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);

-- Create indexes for better query performance
CREATE INDEX idx_links_status ON links(status);
CREATE INDEX idx_links_created_at ON links(created_at DESC);
CREATE INDEX idx_tasks_completed ON tasks(completed);
CREATE INDEX idx_link_tasks_task_id ON link_tasks(task_id);
CREATE INDEX idx_link_categories_category_id ON link_categories(category_id);
CREATE INDEX idx_link_tags_tag_id ON link_tags(tag_id);

-- Create full-text search virtual table for links
CREATE VIRTUAL TABLE links_fts USING fts5(
    url,
    title,
    content,
    summary,
    content=links,
    content_rowid=id
);

-- Create triggers to keep FTS index in sync
CREATE TRIGGER links_fts_insert AFTER INSERT ON links BEGIN INSERT INTO links_fts(rowid, url, title, content, summary) VALUES (new.id, new.url, new.title, new.content, new.summary); END;

CREATE TRIGGER links_fts_update AFTER UPDATE ON links BEGIN UPDATE links_fts SET url = new.url, title = new.title, content = new.content, summary = new.summary WHERE rowid = new.id; END;

CREATE TRIGGER links_fts_delete AFTER DELETE ON links BEGIN DELETE FROM links_fts WHERE rowid = old.id; END;

+goose Down
DROP TRIGGER IF EXISTS links_fts_delete;
DROP TRIGGER IF EXISTS links_fts_update;
DROP TRIGGER IF EXISTS links_fts_insert;
DROP TABLE IF EXISTS links_fts;
DROP INDEX IF EXISTS idx_link_tags_tag_id;
DROP INDEX IF EXISTS idx_link_categories_category_id;
DROP INDEX IF EXISTS idx_link_tasks_task_id;
DROP INDEX IF EXISTS idx_tasks_completed;
DROP INDEX IF EXISTS idx_links_created_at;
DROP INDEX IF EXISTS idx_links_status;
DROP TABLE IF EXISTS link_tags;
DROP TABLE IF EXISTS link_categories;
DROP TABLE IF EXISTS link_tasks;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS links;
