package infrastructure

import (
	"context"
	"database/sql"
	"errors"

	"github.com/FEATO-org/support-feato-system/db/sqlc"
	"github.com/FEATO-org/support-feato-system/domain/model"
	"github.com/FEATO-org/support-feato-system/domain/repository"
)

type SystemUserRepository struct {
	db *sql.DB
}

// Create implements repository.SystemUserRepository.
func (su *SystemUserRepository) Create(ctx context.Context, systemUser *model.SystemUser) (*model.SystemUser, error) {
	tx, err := su.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	queries := sqlc.New(tx)

	result, err := queries.CreateSystemUser(ctx, systemUser.DiscordID)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err = systemUser.Set(id, systemUser.DiscordID, systemUser.Guilds); err != nil {
		return nil, err
	}

	for _, guild := range systemUser.Guilds {
		result, err := queries.FindByDiscordIDGuild(ctx, guild.DiscordID)
		if errors.Is(err, sql.ErrNoRows) {
			created, err := queries.CreateGuild(ctx, sqlc.CreateGuildParams{
				Name:      guild.Name,
				DiscordID: guild.DiscordID,
				SheetID: sql.NullString{
					String: guild.SheetID,
				},
			})
			if err != nil {
				return nil, err
			}

			id, err := created.LastInsertId()
			if err != nil {
				return nil, err
			}
			result.ID = id
			result.Name = guild.Name
		} else if err != nil {
			return nil, err
		}

		err = guild.Set(result.ID, result.Name, guild.DiscordID, guild.SheetID)
		if err != nil {
			return nil, err
		}

		_, err = queries.CreateSystemUserGuild(ctx, sqlc.CreateSystemUserGuildParams{
			SystemUserID: systemUser.Id,
			GuildID:      guild.Id,
		})
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return systemUser, nil
}

func NewSystemUserRepository(db *sql.DB) repository.SystemUserRepository {
	return &SystemUserRepository{db: db}
}
