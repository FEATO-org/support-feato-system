package usecase

import (
	"github.com/FEATO-org/support-feato-system/domain/model"
)

type SystemWSIncomingUsecase interface {
	ReceiveEEW(body interface{}, test bool) (*model.EEW, error)
}

type systemWSIncomingUsecase struct{}

func NewSystemWSIncoming() SystemWSIncomingUsecase {
	return &systemWSIncomingUsecase{}
}

func (su systemWSIncomingUsecase) ReceiveEEW(body interface{}, test bool) (*model.EEW, error) {
	return model.NewEEW(body, test)
}
