package utils

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode"
)

// IsValidEmail checks if the email address is valid
func IsValidEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// IsValidPassword checks if password meets security requirements
func IsValidPassword(password string) bool {
	if len(password) < 8 {
		return false
	}

	var (
		hasUpper   = false
		hasLower   = false
		hasNumber  = false
		hasSpecial = false
	)

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	return hasUpper && hasLower && hasNumber && hasSpecial
}

// IsValidUUID checks if the string is a valid UUID
func IsValidUUID(uuid string) bool {
	r := regexp.MustCompile("^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-4[a-fA-F0-9]{3}-[8|9|aA|bB][a-fA-F0-9]{3}-[a-fA-F0-9]{12}$")
	return r.MatchString(uuid)
}

// IsValidPhoneNumber checks if the phone number is valid (basic check)
func IsValidPhoneNumber(phone string) bool {
	// Remove all non-digit characters
	digits := regexp.MustCompile(`\D`).ReplaceAllString(phone, "")
	
	// Check if it has 10-15 digits (international format)
	return len(digits) >= 10 && len(digits) <= 15
}

// SanitizeString removes potentially dangerous characters from a string
func SanitizeString(input string) string {
	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")
	
	// Trim whitespace
	input = strings.TrimSpace(input)
	
	// Remove control characters except newlines and tabs
	var result strings.Builder
	for _, r := range input {
		if !unicode.IsControl(r) || r == '\n' || r == '\t' {
			result.WriteRune(r)
		}
	}
	
	return result.String()
}

// IsValidURL checks if the string is a valid URL
func IsValidURL(url string) bool {
	urlRegex := regexp.MustCompile(`^https?://[a-zA-Z0-9\-\.]+\.[a-zA-Z]{2,}(/.*)?$`)
	return urlRegex.MatchString(url)
}

// TruncateString truncates a string to the specified length
func TruncateString(s string, length int) string {
	if len(s) <= length {
		return s
	}
	
	if length <= 3 {
		return s[:length]
	}
	
	return s[:length-3] + "..."
}