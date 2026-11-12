package httpapi

import (
	"errors"
	"log"
	"net/http"

	"lingoway/internal/admin"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const adminTopicsMaxBodyBytes = 1 << 14

type adminTopicHTTPHandler struct {
	service *admin.TopicService
}

// registerAdminTopicRoutes подключает управление темами к группе администратора
// (Auth, ActiveUser и RequireRole уже применены в router.go).
func registerAdminTopicRoutes(adminGroup *gin.RouterGroup, database *gorm.DB) {
	handler := adminTopicHTTPHandler{service: admin.NewTopicService(database)}

	group := adminGroup.Group("/topics", privateNoStore())
	{
		group.POST("", handler.create)
		group.PATCH("/:id", handler.rename)
		group.DELETE("/:id", handler.remove)
	}
}

type adminTopicCreateRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// adminTopicPatchRequest: slug принимается только затем, чтобы вернуть понятную ошибку.
type adminTopicPatchRequest struct {
	Name *string `json:"name"`
	Slug *string `json:"slug"`
}

func (h adminTopicHTTPHandler) create(c *gin.Context) {
	var request adminTopicCreateRequest
	if !decodeStrictJSON(c, &request, adminTopicsMaxBodyBytes) {
		return
	}
	topic, err := h.service.Create(c.Request.Context(), request.Slug, request.Name)
	if err != nil {
		writeAdminTopicError(c, err)
		return
	}
	c.JSON(http.StatusCreated, topicResponse{ID: topic.ID, Slug: topic.Slug, Name: topic.Name})
}

func (h adminTopicHTTPHandler) rename(c *gin.Context) {
	topicID, ok := parseCatalogID(c)
	if !ok {
		return
	}
	var request adminTopicPatchRequest
	if !decodeStrictJSON(c, &request, adminTopicsMaxBodyBytes) {
		return
	}
	if request.Slug != nil {
		writeAPIError(c, http.StatusUnprocessableEntity, "INVALID_INPUT", "slug нельзя изменить после создания.")
		return
	}
	if request.Name == nil {
		writeAPIError(c, http.StatusUnprocessableEntity, "INVALID_INPUT", "Укажите name.")
		return
	}
	topic, err := h.service.Rename(c.Request.Context(), topicID, *request.Name)
	if err != nil {
		writeAdminTopicError(c, err)
		return
	}
	c.JSON(http.StatusOK, topicResponse{ID: topic.ID, Slug: topic.Slug, Name: topic.Name})
}

func (h adminTopicHTTPHandler) remove(c *gin.Context) {
	topicID, ok := parseCatalogID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), topicID); err != nil {
		writeAdminTopicError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// writeAdminTopicError не отдаёт клиенту внутренние ошибки, только пишет их в лог.
func writeAdminTopicError(c *gin.Context, err error) {
	var validation *admin.TopicValidationError
	switch {
	case errors.As(err, &validation):
		writeAPIError(c, http.StatusUnprocessableEntity, "INVALID_INPUT", validation.Message)
	case errors.Is(err, admin.ErrTopicNotFound):
		writeAPIError(c, http.StatusNotFound, "NOT_FOUND", "Тема не найдена.")
	case errors.Is(err, admin.ErrTopicSlugExists):
		writeAPIError(c, http.StatusConflict, "TOPIC_SLUG_EXISTS", "Тема с таким slug уже существует.")
	case errors.Is(err, admin.ErrTopicInUse):
		writeAPIError(c, http.StatusConflict, "TOPIC_IN_USE", "Тема используется материалами или модулями и не может быть удалена.")
	default:
		log.Printf("admin topics: %v", err)
		writeAPIError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Не удалось выполнить запрос.")
	}
}
