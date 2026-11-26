package repository

import (
	"context"
	"github.com/FEATO-org/support-feato-system/domain/model"
)

type SystemUserRepository interface {
	Create(context.Context, *model.SystemUser) (*model.SystemUser, error)
}
