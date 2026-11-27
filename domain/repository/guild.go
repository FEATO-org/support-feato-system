package repository

import "github.com/FEATO-org/support-feato-system/domain/model"

type GuildRepository interface {
	Create(guild *model.Guild) (*model.Guild, error)
}
