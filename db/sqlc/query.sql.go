// Code generated by sqlc. DO NOT EDIT.
// source: query.sql

package sqlc

import (
	"context"
	"database/sql"
	"time"
)

const createGuild = `-- name: CreateGuild :execresult
INSERT INTO guilds (name, discord_id)
VALUES (?, ?)
`

type CreateGuildParams struct {
	Name      string
	DiscordID string
}

func (q *Queries) CreateGuild(ctx context.Context, arg CreateGuildParams) (sql.Result, error) {
	return q.db.ExecContext(ctx, createGuild, arg.Name, arg.DiscordID)
}

const createSystemUser = `-- name: CreateSystemUser :execresult
INSERT INTO system_users (discord_id)
VALUES (?)
`

func (q *Queries) CreateSystemUser(ctx context.Context, discordID string) (sql.Result, error) {
	return q.db.ExecContext(ctx, createSystemUser, discordID)
}

const createSystemUserGuild = `-- name: CreateSystemUserGuild :execresult
INSERT INTO system_user_guild (system_user_id, guild_id)
VALUES (?, ?)
`

type CreateSystemUserGuildParams struct {
	SystemUserID int64
	GuildID      int64
}

func (q *Queries) CreateSystemUserGuild(ctx context.Context, arg CreateSystemUserGuildParams) (sql.Result, error) {
	return q.db.ExecContext(ctx, createSystemUserGuild, arg.SystemUserID, arg.GuildID)
}

const createToken = `-- name: CreateToken :execresult
INSERT INTO tokens (
    system_user_id,
    access_token,
    token_type,
    refresh_token,
    expiry
  )
VALUES (?, ?, ?, ?, ?)
`

type CreateTokenParams struct {
	SystemUserID sql.NullInt64
	AccessToken  sql.NullString
	TokenType    string
	RefreshToken string
	Expiry       time.Time
}

func (q *Queries) CreateToken(ctx context.Context, arg CreateTokenParams) (sql.Result, error) {
	return q.db.ExecContext(ctx, createToken,
		arg.SystemUserID,
		arg.AccessToken,
		arg.TokenType,
		arg.RefreshToken,
		arg.Expiry,
	)
}

const deleteGuild = `-- name: DeleteGuild :exec
DELETE FROM guilds
WHERE id = ?
`

func (q *Queries) DeleteGuild(ctx context.Context, id int64) error {
	_, err := q.db.ExecContext(ctx, deleteGuild, id)
	return err
}

const deleteSystemUser = `-- name: DeleteSystemUser :exec
DELETE FROM system_users
WHERE id = ?
`

func (q *Queries) DeleteSystemUser(ctx context.Context, id int64) error {
	_, err := q.db.ExecContext(ctx, deleteSystemUser, id)
	return err
}

const deleteSystemUserGuild = `-- name: DeleteSystemUserGuild :exec
DELETE FROM system_user_guild
WHERE system_user_id = ?
  AND guild_id = ?
`

type DeleteSystemUserGuildParams struct {
	SystemUserID int64
	GuildID      int64
}

func (q *Queries) DeleteSystemUserGuild(ctx context.Context, arg DeleteSystemUserGuildParams) error {
	_, err := q.db.ExecContext(ctx, deleteSystemUserGuild, arg.SystemUserID, arg.GuildID)
	return err
}

const deleteToken = `-- name: DeleteToken :exec
DELETE FROM tokens
WHERE id = ?
`

func (q *Queries) DeleteToken(ctx context.Context, id int64) error {
	_, err := q.db.ExecContext(ctx, deleteToken, id)
	return err
}

const findByDiscordIDGuild = `-- name: FindByDiscordIDGuild :one
SELECT id, name, discord_id, sheet_id, created_at, updated_at
FROM guilds
WHERE discord_id = $1
LIMIT 1
`

func (q *Queries) FindByDiscordIDGuild(ctx context.Context) (Guild, error) {
	row := q.db.QueryRowContext(ctx, findByDiscordIDGuild)
	var i Guild
	err := row.Scan(
		&i.ID,
		&i.Name,
		&i.DiscordID,
		&i.SheetID,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const findByDiscordIDSystemUser = `-- name: FindByDiscordIDSystemUser :one
SELECT id, discord_id, created_at, updated_at
FROM system_users
WHERE discord_id = ?
LIMIT 1
`

func (q *Queries) FindByDiscordIDSystemUser(ctx context.Context, discordID string) (SystemUser, error) {
	row := q.db.QueryRowContext(ctx, findByDiscordIDSystemUser, discordID)
	var i SystemUser
	err := row.Scan(
		&i.ID,
		&i.DiscordID,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const findByGuildIDSystemUserGuild = `-- name: FindByGuildIDSystemUserGuild :many
SELECT system_user_id, guild_id, created_at, updated_at
FROM system_user_guild
WHERE guild_id = ?
`

func (q *Queries) FindByGuildIDSystemUserGuild(ctx context.Context, guildID int64) ([]SystemUserGuild, error) {
	rows, err := q.db.QueryContext(ctx, findByGuildIDSystemUserGuild, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []SystemUserGuild
	for rows.Next() {
		var i SystemUserGuild
		if err := rows.Scan(
			&i.SystemUserID,
			&i.GuildID,
			&i.CreatedAt,
			&i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const findByIDGuild = `-- name: FindByIDGuild :one
SELECT id, name, discord_id, sheet_id, created_at, updated_at
FROM guilds
WHERE id = ?
LIMIT 1
`

func (q *Queries) FindByIDGuild(ctx context.Context, id int64) (Guild, error) {
	row := q.db.QueryRowContext(ctx, findByIDGuild, id)
	var i Guild
	err := row.Scan(
		&i.ID,
		&i.Name,
		&i.DiscordID,
		&i.SheetID,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const findByIDSystemUser = `-- name: FindByIDSystemUser :one
SELECT id, discord_id, created_at, updated_at
FROM system_users
WHERE id = ?
LIMIT 1
`

func (q *Queries) FindByIDSystemUser(ctx context.Context, id int64) (SystemUser, error) {
	row := q.db.QueryRowContext(ctx, findByIDSystemUser, id)
	var i SystemUser
	err := row.Scan(
		&i.ID,
		&i.DiscordID,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const findByIDSystemUserGuild = `-- name: FindByIDSystemUserGuild :one
SELECT system_user_id, guild_id, created_at, updated_at
FROM system_user_guild
WHERE guild_id = ?
  AND system_user_id = ?
`

type FindByIDSystemUserGuildParams struct {
	GuildID      int64
	SystemUserID int64
}

func (q *Queries) FindByIDSystemUserGuild(ctx context.Context, arg FindByIDSystemUserGuildParams) (SystemUserGuild, error) {
	row := q.db.QueryRowContext(ctx, findByIDSystemUserGuild, arg.GuildID, arg.SystemUserID)
	var i SystemUserGuild
	err := row.Scan(
		&i.SystemUserID,
		&i.GuildID,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const findByIDToken = `-- name: FindByIDToken :one
SELECT id, system_user_id, access_token, token_type, refresh_token, expiry, created_at, updated_at
FROM tokens
WHERE id = ?
LIMIT 1
`

func (q *Queries) FindByIDToken(ctx context.Context, id int64) (Token, error) {
	row := q.db.QueryRowContext(ctx, findByIDToken, id)
	var i Token
	err := row.Scan(
		&i.ID,
		&i.SystemUserID,
		&i.AccessToken,
		&i.TokenType,
		&i.RefreshToken,
		&i.Expiry,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}

const findBySystemUserIDSystemUserGuild = `-- name: FindBySystemUserIDSystemUserGuild :many
SELECT system_user_id, guild_id, created_at, updated_at
FROM system_user_guild
WHERE system_user_id = ?
`

func (q *Queries) FindBySystemUserIDSystemUserGuild(ctx context.Context, systemUserID int64) ([]SystemUserGuild, error) {
	rows, err := q.db.QueryContext(ctx, findBySystemUserIDSystemUserGuild, systemUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []SystemUserGuild
	for rows.Next() {
		var i SystemUserGuild
		if err := rows.Scan(
			&i.SystemUserID,
			&i.GuildID,
			&i.CreatedAt,
			&i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const findByUserIDToken = `-- name: FindByUserIDToken :one
SELECT id, system_user_id, access_token, token_type, refresh_token, expiry, created_at, updated_at
FROM tokens
WHERE system_user_id = ?
LIMIT 1
`

func (q *Queries) FindByUserIDToken(ctx context.Context, systemUserID sql.NullInt64) (Token, error) {
	row := q.db.QueryRowContext(ctx, findByUserIDToken, systemUserID)
	var i Token
	err := row.Scan(
		&i.ID,
		&i.SystemUserID,
		&i.AccessToken,
		&i.TokenType,
		&i.RefreshToken,
		&i.Expiry,
		&i.CreatedAt,
		&i.UpdatedAt,
	)
	return i, err
}
