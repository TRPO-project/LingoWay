package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"lingoway/internal/catalog"
	"lingoway/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Тело запроса ограничено 1 МиБ: текст материала до 100 000 символов
// (до 4 байт на символ в UTF-8) в него помещается.
const adminCatalogMaxBodyBytes = 1 << 20

type adminCatalogHTTPHandler struct {
	service *catalog.Service
}

// registerAdminCatalogRoutes подключает управление каталогом. Группа admin уже
// требует вход и роль администратора (Auth и RequireRole в router.go).
func registerAdminCatalogRoutes(admin *gin.RouterGroup, database *gorm.DB) {
	handler := adminCatalogHTTPHandler{service: catalog.NewService(database)}

	group := admin.Group("/catalog", privateNoStore())
	{
		group.GET("/materials", handler.list)
		group.POST("/materials", handler.create)
		group.GET("/materials/:id", handler.get)
		group.PUT("/materials/:id", handler.replace)
		group.DELETE("/materials/:id", handler.remove)
	}
}

// --- запросы и ответы ---

// adminCatalogRequest — тело создания и полной замены. Неизвестные поля запрещены.
type adminCatalogRequest struct {
	TopicID     int64                      `json:"topic_id"`
	Level       models.CEFRLevel           `json:"level"`
	Type        models.CatalogMaterialType `json:"type"`
	Title       string                     `json:"title"`
	Description *string                    `json:"description"`
	Body        *string                    `json:"body"`
	URL         *string                    `json:"url"`
}

type adminMaterialItemResponse struct {
	ID          int64                      `json:"id"`
	TopicID     int64                      `json:"topic_id"`
	Level       models.CEFRLevel           `json:"level"`
	Type        models.CatalogMaterialType `json:"type"`
	Title       string                     `json:"title"`
	Description *string                    `json:"description"`
	URL         *string                    `json:"url"`
	CreatedAt   time.Time                  `json:"created_at"`
}

type adminMaterialResponse struct {
	adminMaterialItemResponse
	Body      *string   `json:"body"`
	UpdatedAt time.Time `json:"updated_at"`
}

func newAdminMaterialResponse(detail *catalog.MaterialDetail) adminMaterialResponse {
	return adminMaterialResponse{
		adminMaterialItemResponse: adminMaterialItemResponse{
			ID: detail.ID, TopicID: detail.TopicID, Level: detail.Level, Type: detail.Type,
			Title: detail.Title, Description: detail.Description, URL: detail.URL,
			CreatedAt: detail.CreatedAt,
		},
		Body:      detail.Body,
		UpdatedAt: detail.UpdatedAt,
	}
}

// --- обработчики ---

func (h adminCatalogHTTPHandler) list(c *gin.Context) {
	filter, ok := parseCatalogFilter(c)
	if !ok {
		return
	}
	// Избранное относится к пользователям, в админском списке оно не используется.
	filter.Favorite = nil
	items, hasMore, err := h.service.ListMaterials(c.Request.Context(), 0, filter)
	if err != nil {
		writeAdminCatalogError(c, err)
		return
	}
	response := make([]adminMaterialItemResponse, 0, len(items))
	for _, item := range items {
		response = append(response, adminMaterialItemResponse{
			ID: item.ID, TopicID: item.TopicID, Level: item.Level, Type: item.Type,
			Title: item.Title, Description: item.Description, URL: item.URL,
			CreatedAt: item.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, catalogPage[adminMaterialItemResponse]{
		Items: response, Limit: filter.Limit, Offset: filter.Offset, HasMore: hasMore,
	})
}

func (h adminCatalogHTTPHandler) get(c *gin.Context) {
	materialID, ok := parseCatalogID(c)
	if !ok {
		return
	}
	detail, err := h.service.GetMaterial(c.Request.Context(), 0, materialID)
	if err != nil {
		writeAdminCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, newAdminMaterialResponse(detail))
}

func (h adminCatalogHTTPHandler) create(c *gin.Context) {
	input, ok := decodeAdminCatalogInput(c)
	if !ok {
		return
	}
	detail, err := h.service.CreateMaterial(c.Request.Context(), input)
	if err != nil {
		writeAdminCatalogError(c, err)
		return
	}
	c.JSON(http.StatusCreated, newAdminMaterialResponse(detail))
}

func (h adminCatalogHTTPHandler) replace(c *gin.Context) {
	materialID, ok := parseCatalogID(c)
	if !ok {
		return
	}
	input, ok := decodeAdminCatalogInput(c)
	if !ok {
		return
	}
	detail, err := h.service.UpdateMaterial(c.Request.Context(), materialID, input)
	if err != nil {
		writeAdminCatalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, newAdminMaterialResponse(detail))
}

func (h adminCatalogHTTPHandler) remove(c *gin.Context) {
	materialID, ok := parseCatalogID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteMaterial(c.Request.Context(), materialID); err != nil {
		writeAdminCatalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// --- разбор запроса и ошибки ---

// decodeAdminCatalogInput читает тело строго: один JSON-объект, без неизвестных
// полей и лишних данных после него. Данные проходят проверку Normalize.
func decodeAdminCatalogInput(c *gin.Context) (catalog.MaterialInput, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminCatalogMaxBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()

	var request adminCatalogRequest
	err := decoder.Decode(&request)
	if err == nil {
		if extra := decoder.Decode(&struct{}{}); !errors.Is(extra, io.EOF) {
			err = errors.New("unexpected data after JSON object")
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeAPIError(c, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Тело запроса слишком большое.")
		} else {
			writeAPIError(c, http.StatusBadRequest, "INVALID_JSON", "Проверьте JSON и набор полей запроса.")
		}
		return catalog.MaterialInput{}, false
	}

	input := catalog.MaterialInput{
		TopicID: request.TopicID, Level: request.Level, Type: request.Type,
		Title: request.Title, Description: request.Description,
		Body: request.Body, URL: request.URL,
	}
	if err := input.Normalize(); err != nil {
		writeAdminCatalogError(c, err)
		return catalog.MaterialInput{}, false
	}
	return input, true
}

func writeAdminCatalogError(c *gin.Context, err error) {
	var validation *catalog.ValidationError
	switch {
	case errors.As(err, &validation):
		writeAPIError(c, http.StatusUnprocessableEntity, "INVALID_INPUT", validation.Message)
	case errors.Is(err, catalog.ErrTopicNotFound):
		writeAPIError(c, http.StatusUnprocessableEntity, "TOPIC_NOT_FOUND", "Тема не найдена.")
	default:
		writeCatalogError(c, err)
	}
}
