package usecase

import (
	"context"
	"github.com/FEATO-org/support-feato-system/domain/model"
	"github.com/FEATO-org/support-feato-system/domain/repository"
)

type SystemUser struct {
	systemUserRepository repository.SystemUserRepository
}

func NewSystemUser(r repository.SystemUserRepository) *SystemUser {
	return &SystemUser{systemUserRepository: r}
}

func (du *SystemUser) CreateSystemUser(ctx context.Context, userDiscordID, guildDiscordID, sheetId, guildName string) (*model.SystemUser, error) {
	guildModel, err := model.NewGuild(0, guildName, guildDiscordID, sheetId)
	if err != nil {
		return nil, err
	}
	model, err := model.NewSystemUser(0, userDiscordID, []model.Guild{*guildModel})
	if err != nil {
		return nil, err
	}

	return du.systemUserRepository.Create(ctx, model)
}
