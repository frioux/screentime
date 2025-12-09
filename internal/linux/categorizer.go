package linux

import (
	"strings"
)

// Categorizer categorizes URLs based on domain patterns
type Categorizer struct {
	categories map[string]Category
}

// NewCategorizer creates a new categorizer from config
func NewCategorizer(categories map[string]Category) *Categorizer {
	return &Categorizer{categories: categories}
}

// Categorize returns the category for a given domain
// Returns "uncategorized" if no match is found
func (c *Categorizer) Categorize(domain string) string {
	if domain == "" {
		return "uncategorized"
	}

	domainLower := strings.ToLower(domain)

	for categoryName, category := range c.categories {
		// Check exact domain matches
		for _, d := range category.Domains {
			if strings.ToLower(d) == domainLower {
				return categoryName
			}
			// Also check if domain ends with the pattern (for subdomains)
			if strings.HasSuffix(domainLower, "."+strings.ToLower(d)) {
				return categoryName
			}
		}

		// Check domain suffixes
		for _, suffix := range category.DomainSuffixes {
			if strings.HasSuffix(domainLower, strings.ToLower(suffix)) {
				return categoryName
			}
		}
	}

	return "uncategorized"
}


