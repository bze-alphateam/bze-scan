package dto

import "github.com/bze-alphateam/bze-scan/backend/app/repository"

// ModuleAccount is an item of GET /api/v1/modules: a module account's
// address, its display name and the module that owns it.
type ModuleAccount struct {
	Address string `json:"address"`
	Name    string `json:"name"`
	Module  string `json:"module"`
}

// ModuleList is GET /api/v1/modules: every module account, by name. The
// list is short (one row per module the chain declares), so it has no
// pages and no cursor.
type ModuleList struct {
	Items []ModuleAccount `json:"items"`
}

// NewModuleAccount maps a module account label.
func NewModuleAccount(m repository.ModuleAccount) ModuleAccount {
	return ModuleAccount{Address: m.Address, Name: m.Name, Module: m.Module}
}
