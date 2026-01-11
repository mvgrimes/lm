-- +goose Up
-- Create activities table
CREATE TABLE activities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create link_activities junction table (many-to-many)
CREATE TABLE link_activities (
    link_id INTEGER NOT NULL,
    activity_id INTEGER NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (link_id, activity_id),
    FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE CASCADE,
    FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE
);

-- Indexes
CREATE INDEX idx_link_activities_activity_id ON link_activities(activity_id);

-- +goose Down
DROP INDEX IF EXISTS idx_link_activities_activity_id;
DROP TABLE IF EXISTS link_activities;
DROP TABLE IF EXISTS activities;
