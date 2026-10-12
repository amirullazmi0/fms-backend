package validator

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

var validate *validator.Validate

// InitValidator initializes the validator with custom rules
func InitValidator() {
	validate = validator.New()

	// Register custom validators
	validate.RegisterValidation("phone_id", validatePhoneID)
	validate.RegisterValidation("postal_code_id", validatePostalCodeID)
	validate.RegisterValidation("future_date", validateFutureDate)
	validate.RegisterValidation("hhmm", validateHHMM)

	// Report errors with the JSON field name (e.g. "loadingId"), which is what the frontend sends.
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" || name == "" {
			return fld.Name
		}
		return name
	})
}

var hhmmPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// validateHHMM validates a 24h clock time "HH:MM"
func validateHHMM(fl validator.FieldLevel) bool {
	return hhmmPattern.MatchString(fl.Field().String())
}

// validatePhoneID validates Indonesian phone number format
func validatePhoneID(fl validator.FieldLevel) bool {
	phone := fl.Field().String()
	// Indonesian phone format: 08xx, 628xx, +628xx (9-13 digits after prefix)
	matched, _ := regexp.MatchString(`^(\+62|62|0)[0-9]{9,13}$`, phone)
	return matched
}

// validatePostalCodeID validates Indonesian postal code (5 digits)
func validatePostalCodeID(fl validator.FieldLevel) bool {
	postalCode := fl.Field().String()
	matched, _ := regexp.MatchString(`^[0-9]{5}$`, postalCode)
	return matched
}

// validateFutureDate validates that date is in the future
func validateFutureDate(fl validator.FieldLevel) bool {
	dateStr := fl.Field().String()
	if dateStr == "" {
		return true // allow empty for optional fields
	}

	// Parse ISO 8601 format
	date, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return false
	}

	return date.After(time.Now())
}

// Validate validates a struct
func Validate(data interface{}) error {
	if validate == nil {
		InitValidator()
	}
	return validate.Struct(data)
}

// FormatValidationErrors formats validation errors into a readable map
func FormatValidationErrors(err error) map[string]string {
	errors := make(map[string]string)

	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		for _, e := range validationErrors {
			field := e.Field()
			errors[field] = formatErrorMessage(e)
		}
	}

	return errors
}

func formatErrorMessage(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", e.Field())
	case "email":
		return "Invalid email format"
	case "min":
		return fmt.Sprintf("%s must be at least %s characters", e.Field(), e.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s characters", e.Field(), e.Param())
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", e.Field(), e.Param())
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", e.Field(), e.Param())
	case "lte":
		return fmt.Sprintf("%s must be less than or equal to %s", e.Field(), e.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", e.Field(), e.Param())
	case "url":
		return "Invalid URL format"
	case "uuid4":
		return "Invalid UUID format"
	case "datetime":
		return fmt.Sprintf("%s must use %s format", e.Field(), e.Param())
	case "phone_id":
		return "Invalid Indonesian phone number format"
	case "postal_code_id":
		return "Invalid postal code (must be 5 digits)"
	case "future_date":
		return "Date must be in the future"
	case "hhmm":
		return fmt.Sprintf("%s must use HH:MM format", e.Field())
	case "uuid":
		return fmt.Sprintf("%s must be a valid id", e.Field())
	case "nefield":
		return fmt.Sprintf("%s must differ from %s", e.Field(), e.Param())
	case "gtfield":
		return fmt.Sprintf("%s must be after %s", e.Field(), e.Param())
	case "len":
		return fmt.Sprintf("%s must be %s characters", e.Field(), e.Param())
	default:
		return fmt.Sprintf("%s is invalid", e.Field())
	}
}
