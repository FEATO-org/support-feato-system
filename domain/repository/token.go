package repository

import "github.com/FEATO-org/support-feato-system/domain/model"

type TokenRepository interface {
	Create(token *model.Token) (*model.Token, error)
}
