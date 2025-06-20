package services

import (
	"kapi/models"

	"gorm.io/gorm"
)

type UserService struct {
	db *gorm.DB
}

func NewUserService(db *gorm.DB) *UserService {
	return &UserService{db: db}
}

func (s *UserService) CreateUser(req *models.CreateUserRequest) (*models.User, error) {
	user := &models.User{
		Email:     req.Email,
		Username:  req.Username,
		Password:  req.Password,
		FirstName: req.FirstName,
		LastName:  req.LastName,
	}

	if err := user.HashPassword(); err != nil {
		return nil, err
	}

	if err := s.db.Create(user).Error; err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserService) GetAllUsers() ([]models.User, error) {
	var users []models.User
	err := s.db.Find(&users).Error
	return users, err
}

func (s *UserService) GetUserByID(id uint) (*models.User, error) {
	var user models.User
	err := s.db.Preload("Posts").First(&user, id).Error
	return &user, err
}

func (s *UserService) GetUserByEmail(email string) (*models.User, error) {
	var user models.User
	err := s.db.Where("email = ?", email).First(&user).Error
	return &user, err
}

func (s *UserService) UpdateUser(id uint, req *models.UpdateUserRequest) (*models.User, error) {
	var user models.User
	if err := s.db.First(&user, id).Error; err != nil {
		return nil, err
	}

	if req.FirstName != "" {
		user.FirstName = req.FirstName
	}
	if req.LastName != "" {
		user.LastName = req.LastName
	}
	if req.Username != "" {
		user.Username = req.Username
	}

	if err := s.db.Save(&user).Error; err != nil {
		return nil, err
	}

	return &user, nil
}

func (s *UserService) DeleteUser(id uint) error {
	return s.db.Delete(&models.User{}, id).Error
}

func (us *UserService) UpdateOpenRouterKey(userID uint, key string) error {
	var user models.User
	if err := us.db.Where("id = ?", userID).First(&user).Error; err != nil {
		return err
	}

	if err := user.EncryptOpenRouterKey(key); err != nil {
		return err
	}

	result := us.db.Model(&user).Update("openrouter_key", user.OpenRouterKey)
	return result.Error
}

func (us *UserService) GetUserOpenRouterKey(userID uint) (string, error) {
	var user models.User
	result := us.db.Select("openrouter_key").Where("id = ?", userID).First(&user)
	if result.Error != nil {
		return "", result.Error
	}

	return user.DecryptOpenRouterKey()
}

func (us *UserService) HasOpenRouterKey(userID uint) (bool, error) {
	var count int64
	result := us.db.Model(&models.User{}).Where("id = ? AND openrouter_key IS NOT NULL AND openrouter_key != ''", userID).Count(&count)
	if result.Error != nil {
		return false, result.Error
	}
	return count > 0, nil
}
