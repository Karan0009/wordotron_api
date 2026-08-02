package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/app/config"
	"github.com/Karan0009/wordotron_api/app/lib"
	"github.com/Karan0009/wordotron_api/app/lib/validation"
	"github.com/Karan0009/wordotron_api/app/models"
	"github.com/Karan0009/wordotron_api/app/serializers"
	"github.com/Karan0009/wordotron_api/app/services"
)

// userSortWhitelist restricts ?sort= to indexed, non-sensitive columns.
var userSortWhitelist = lib.SortWhitelist{
	Allowed: []string{"created_at", "updated_at", "email", "full_name"},
	Default: "created_at",
}

// UserHandler exposes the user management endpoints.
type UserHandler struct {
	base
	users services.UserService
	cfg   *config.Config
}

// NewUserHandler builds the user handler.
func NewUserHandler(users services.UserService, validator *validation.Validator, cfg *config.Config) *UserHandler {
	return &UserHandler{
		base:  base{validator: validator},
		users: users,
		cfg:   cfg,
	}
}

// Me returns the authenticated user.
//
//	@Summary		Get the current user
//	@Tags			users
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	lib.SuccessEnvelope{data=UserResponse}
//	@Failure		401	{object}	lib.ErrorEnvelope
//	@Router			/users/me [get]
func (h *UserHandler) Me(c fiber.Ctx) error {
	actor, err := actorFrom(c)
	if err != nil {
		return err
	}

	user, err := h.users.Get(c.Context(), actor, actor.ID)
	if err != nil {
		return err
	}
	return lib.OK(c, serializers.ToUserResponse(user))
}

// UpdateMe updates the authenticated user's own profile.
//
//	@Summary		Update the current user
//	@Description	Role and activation changes are rejected for non-administrators.
//	@Tags			users
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		serializers.UpdateUserRequest	true	"Fields to update"
//	@Success		200		{object}	lib.SuccessEnvelope{data=UserResponse}
//	@Failure		403		{object}	lib.ErrorEnvelope
//	@Router			/users/me [patch]
func (h *UserHandler) UpdateMe(c fiber.Ctx) error {
	actor, err := actorFrom(c)
	if err != nil {
		return err
	}
	return h.update(c, actor, actor.ID)
}

// UploadAvatar stores a new profile picture for the authenticated user.
//
//	@Summary		Upload an avatar
//	@Description	Accepts a multipart form with an "avatar" file field (JPEG, PNG, WebP or GIF).
//	@Tags			users
//	@Security		BearerAuth
//	@Accept			mpfd
//	@Produce		json
//	@Param			avatar	formData	file	true	"Image file"
//	@Success		200		{object}	lib.SuccessEnvelope{data=UserResponse}
//	@Failure		413		{object}	lib.ErrorEnvelope
//	@Failure		415		{object}	lib.ErrorEnvelope
//	@Router			/users/me/avatar [post]
func (h *UserHandler) UploadAvatar(c fiber.Ctx) error {
	actor, err := actorFrom(c)
	if err != nil {
		return err
	}

	header, err := c.FormFile("avatar")
	if err != nil {
		return lib.BadRequest("An avatar file is required").
			WithFields(lib.FieldError{Field: "avatar", Message: "This field is required"}).Wrap(err)
	}
	if header.Size > h.cfg.Storage.MaxUploadBytes {
		return lib.PayloadTooLarge("Avatar exceeds the maximum upload size")
	}

	file, err := header.Open()
	if err != nil {
		return lib.BadRequest("The uploaded file could not be read").Wrap(err)
	}
	defer func() { _ = file.Close() }()

	user, err := h.users.UploadAvatar(c.Context(), actor, actor.ID, services.AvatarInput{
		Filename:    header.Filename,
		ContentType: header.Header.Get(fiber.HeaderContentType),
		Size:        header.Size,
		Content:     file,
	})
	if err != nil {
		return err
	}

	return lib.OK(c, serializers.ToUserResponse(user))
}

// List returns a paginated, filtered page of users.
//
//	@Summary		List users
//	@Description	Administrators only. Supports page, limit, sort, order, search, role and is_active.
//	@Tags			users
//	@Security		BearerAuth
//	@Produce		json
//	@Param			page		query		int		false	"Page number"	default(1)
//	@Param			limit		query		int		false	"Page size (max 100)"	default(20)
//	@Param			sort		query		string	false	"Sort column"	Enums(created_at, updated_at, email, full_name)
//	@Param			order		query		string	false	"Sort direction"	Enums(asc, desc)
//	@Param			search		query		string	false	"Match against email or full name"
//	@Param			role		query		string	false	"Filter by role"	Enums(user, admin)
//	@Param			is_active	query		bool	false	"Filter by activation state"
//	@Success		200			{object}	lib.SuccessEnvelope{data=[]UserResponse,meta=models.PageMeta}
//	@Failure		403			{object}	lib.ErrorEnvelope
//	@Router			/users [get]
func (h *UserHandler) List(c fiber.Ctx) error {
	page, err := lib.ParsePageParams(c, userSortWhitelist)
	if err != nil {
		return err
	}

	filter := models.ListUsersFilter{Page: page}

	if raw := c.Query("role"); raw != "" {
		role := models.Role(raw)
		if !role.Valid() {
			return lib.Validation([]lib.FieldError{
				{Field: "role", Message: "Must be one of: user, admin"},
			})
		}
		filter.Role = &role
	}

	if raw := c.Query("is_active"); raw != "" {
		switch raw {
		case "true":
			value := true
			filter.IsActive = &value
		case "false":
			value := false
			filter.IsActive = &value
		default:
			return lib.Validation([]lib.FieldError{
				{Field: "is_active", Message: "Must be true or false"},
			})
		}
	}

	result, err := h.users.List(c.Context(), filter)
	if err != nil {
		return err
	}

	return lib.Paginated(c, models.Page[serializers.UserResponse]{
		Items: serializers.ToUserResponses(result.Items),
		Meta:  result.Meta,
	})
}

// Create adds a user with an explicit role.
//
//	@Summary		Create a user
//	@Description	Administrators only.
//	@Tags			users
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			payload	body		serializers.CreateUserRequest	true	"New user"
//	@Success		201		{object}	lib.SuccessEnvelope{data=UserResponse}
//	@Failure		409		{object}	lib.ErrorEnvelope
//	@Router			/users [post]
func (h *UserHandler) Create(c fiber.Ctx) error {
	var req serializers.CreateUserRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	user, err := h.users.Create(c.Context(), services.CreateUserInput{
		Email:    req.Email,
		Password: req.Password,
		FullName: req.FullName,
		Role:     models.Role(req.Role),
	})
	if err != nil {
		return err
	}

	return lib.Created(c, serializers.ToUserResponse(user))
}

// Get returns a single user by ID.
//
//	@Summary		Get a user
//	@Description	Administrators can read any account; other callers only their own.
//	@Tags			users
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"User ID"	format(uuid)
//	@Success		200	{object}	lib.SuccessEnvelope{data=UserResponse}
//	@Failure		404	{object}	lib.ErrorEnvelope
//	@Router			/users/{id} [get]
func (h *UserHandler) Get(c fiber.Ctx) error {
	actor, err := actorFrom(c)
	if err != nil {
		return err
	}

	id, err := uuidParam(c, "id")
	if err != nil {
		return err
	}

	user, err := h.users.Get(c.Context(), actor, id)
	if err != nil {
		return err
	}
	return lib.OK(c, serializers.ToUserResponse(user))
}

// Update applies a partial update to a user.
//
//	@Summary		Update a user
//	@Tags			users
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"User ID"	format(uuid)
//	@Param			payload	body		serializers.UpdateUserRequest	true	"Fields to update"
//	@Success		200		{object}	lib.SuccessEnvelope{data=UserResponse}
//	@Failure		403		{object}	lib.ErrorEnvelope
//	@Router			/users/{id} [patch]
func (h *UserHandler) Update(c fiber.Ctx) error {
	actor, err := actorFrom(c)
	if err != nil {
		return err
	}

	id, err := uuidParam(c, "id")
	if err != nil {
		return err
	}
	return h.update(c, actor, id)
}

// Delete removes a user.
//
//	@Summary		Delete a user
//	@Description	Administrators can delete any account except their own; other callers only their own.
//	@Tags			users
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"User ID"	format(uuid)
//	@Success		200	{object}	lib.SuccessEnvelope
//	@Failure		403	{object}	lib.ErrorEnvelope
//	@Router			/users/{id} [delete]
func (h *UserHandler) Delete(c fiber.Ctx) error {
	actor, err := actorFrom(c)
	if err != nil {
		return err
	}

	id, err := uuidParam(c, "id")
	if err != nil {
		return err
	}

	if err := h.users.Delete(c.Context(), actor, id); err != nil {
		return err
	}
	return lib.Message(c, fiber.StatusOK, "User deleted")
}

// update is shared by the /users/me and /users/:id paths.
func (h *UserHandler) update(c fiber.Ctx, actor services.Actor, id uuid.UUID) error {
	var req serializers.UpdateUserRequest
	if err := h.bind(c, &req); err != nil {
		return err
	}

	in := services.UpdateUserInput{
		FullName: req.FullName,
		IsActive: req.IsActive,
	}
	if req.Role != nil {
		role := models.Role(*req.Role)
		in.Role = &role
	}

	user, err := h.users.Update(c.Context(), actor, id, in)
	if err != nil {
		return err
	}
	return lib.OK(c, serializers.ToUserResponse(user))
}
