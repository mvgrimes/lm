# Link Manager

This is a tool to manage links, shortcuts, knowlege, etc.

It is written in go, using sqlite3 as a database, and the
charm framework for the user interface. It should not require cgo.
We'll use sqlc to generate the sql queries and goose to manage database
migrations.

We are using Cobra to generate the CLI interface. We will later add
additional commands (probably a web interface), but for now the
default command should start the TUI.

We want to accept links as input, and store them in the database. We will
fetch text from the link and store that in the database for searching. We
should also parse the text using an LLM (openai api) and store a summary
in the database for searching.

The primary interface should have a few modes:

- Add links

- Tasks
    List tasks (ie, pay bills, research, etc)
    When a task is selected, the list of links assocuated with that task is displayed,
    and the user can select to open all the links in a new browser window.

- Read Later
    List of links (with summary) to that we want to read.

- Remember
    List of links (with summary) that we want to categorize, tag, etc.

- Search

