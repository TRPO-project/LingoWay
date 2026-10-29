package materials

import (
	"errors"

	"lingoway/internal/models/users"

	"gorm.io/gorm"
)

const sharedMaterialOwnerEmail = "shared-materials@lingoway.invalid"

// findOrCreateSharedMaterialOwner сохраняет обязательные связи с users без
// проверки отправителя. Все новые материалы общего режима имеют одного владельца.
// Блокировка до конца транзакции предотвращает создание двух служебных записей
// при одновременных запросах или запуске нескольких экземпляров приложения.
func findOrCreateSharedMaterialOwner(transaction *gorm.DB) (int64, error) {
	if err := transaction.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "materials:shared-owner").Error; err != nil {
		return 0, err
	}
	var sharedOwner users.User
	lookupErr := transaction.Where("lower(btrim(email)) = ?", sharedMaterialOwnerEmail).First(&sharedOwner).Error
	if lookupErr == nil {
		return sharedOwner.ID, nil
	}
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return 0, lookupErr
	}
	sharedOwner = users.User{
		Email: sharedMaterialOwnerEmail, Name: "Общие материалы", Role: users.RoleStudent,
		// Это не хеш пароля для входа. Будущая проверка bcrypt его не примет:
		// служебная запись нужна только для связей материалов и очереди в БД.
		PasswordHash: "!login-disabled:shared-material-owner",
	}
	if err := transaction.Create(&sharedOwner).Error; err != nil {
		return 0, err
	}
	return sharedOwner.ID, nil
}
