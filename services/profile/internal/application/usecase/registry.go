package usecase

import (
	"profile/internal/application/service"
	"profile/internal/application/usecase/profile/discount"
	"profile/internal/application/usecase/profile/get"
	"profile/internal/application/usecase/profile/list"
	"profile/internal/application/usecase/profile/update"
	"profile/internal/infrastructure/adapter"
	"profile/internal/infrastructure/storage"
)

type Registry struct {
	GetDiscount   *discount.UseCase
	GetProfile    *get.UseCase
	ListProfiles  *list.UseCase
	UpdateProfile *update.UseCase
}

func NewRegistry(storage *storage.Registry, services *service.Registry, adapters *adapter.Registry) *Registry {
	return &Registry{
		GetDiscount:   discount.New(adapters.Discounts),
		GetProfile:    get.New(storage.Profiles),
		ListProfiles:  list.New(storage.Profiles),
		UpdateProfile: update.New(storage.Profiles, services.UpdateProfile),
	}
}
