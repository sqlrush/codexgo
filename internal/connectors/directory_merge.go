package connectors

import (
	"sort"
	"strings"
)

// mergeDirectoryApps groups directory apps by id, folding duplicates into the
// first-seen entry. The Rust HashMap into_values() order is nondeterministic,
// but callers always re-sort by (name, id) afterward, so deterministic id-order
// iteration here is observationally equivalent. Rust: merge_directory_apps.
func mergeDirectoryApps(apps []DirectoryApp) []DirectoryApp {
	merged := make(map[string]*DirectoryApp)
	order := make([]string, 0, len(apps))
	for i := range apps {
		app := apps[i]
		if existing, ok := merged[app.ID]; ok {
			mergeDirectoryApp(existing, app)
		} else {
			a := app
			merged[app.ID] = &a
			order = append(order, app.ID)
		}
	}
	sort.Strings(order)
	out := make([]DirectoryApp, 0, len(merged))
	for _, id := range order {
		out = append(out, *merged[id])
	}
	return out
}

// mergeDirectoryApp folds an incoming duplicate into an existing app, filling in
// only absent/blank fields and OR-ing the discoverable flag. Rust:
// merge_directory_app.
func mergeDirectoryApp(existing *DirectoryApp, incoming DirectoryApp) {
	incomingNameIsEmpty := strings.TrimSpace(incoming.Name) == ""
	if strings.TrimSpace(existing.Name) == "" && !incomingNameIsEmpty {
		existing.Name = incoming.Name
	}

	if incoming.Description != nil && strings.TrimSpace(*incoming.Description) != "" {
		existing.Description = incoming.Description
	}

	if existing.LogoURL == nil && incoming.LogoURL != nil {
		existing.LogoURL = incoming.LogoURL
	}
	if existing.LogoURLDark == nil && incoming.LogoURLDark != nil {
		existing.LogoURLDark = incoming.LogoURLDark
	}
	if existing.DistributionChannel == nil && incoming.DistributionChannel != nil {
		existing.DistributionChannel = incoming.DistributionChannel
	}

	if incoming.Branding != nil {
		if existing.Branding != nil {
			eb, ib := existing.Branding, incoming.Branding
			if eb.Category == nil && ib.Category != nil {
				eb.Category = ib.Category
			}
			if eb.Developer == nil && ib.Developer != nil {
				eb.Developer = ib.Developer
			}
			if eb.Website == nil && ib.Website != nil {
				eb.Website = ib.Website
			}
			if eb.PrivacyPolicy == nil && ib.PrivacyPolicy != nil {
				eb.PrivacyPolicy = ib.PrivacyPolicy
			}
			if eb.TermsOfService == nil && ib.TermsOfService != nil {
				eb.TermsOfService = ib.TermsOfService
			}
			if !eb.IsDiscoverableApp && ib.IsDiscoverableApp {
				eb.IsDiscoverableApp = true
			}
		} else {
			existing.Branding = incoming.Branding
		}
	}

	if incoming.AppMetadata != nil {
		if existing.AppMetadata != nil {
			em, im := existing.AppMetadata, incoming.AppMetadata
			if em.Review == nil && im.Review != nil {
				em.Review = im.Review
			}
			if em.Categories == nil && im.Categories != nil {
				em.Categories = im.Categories
			}
			if em.SubCategories == nil && im.SubCategories != nil {
				em.SubCategories = im.SubCategories
			}
			if em.SeoDescription == nil && im.SeoDescription != nil {
				em.SeoDescription = im.SeoDescription
			}
			if em.Screenshots == nil && im.Screenshots != nil {
				em.Screenshots = im.Screenshots
			}
			if em.Developer == nil && im.Developer != nil {
				em.Developer = im.Developer
			}
			if em.Version == nil && im.Version != nil {
				em.Version = im.Version
			}
			if em.VersionID == nil && im.VersionID != nil {
				em.VersionID = im.VersionID
			}
			if em.VersionNotes == nil && im.VersionNotes != nil {
				em.VersionNotes = im.VersionNotes
			}
			if em.FirstPartyType == nil && im.FirstPartyType != nil {
				em.FirstPartyType = im.FirstPartyType
			}
			if em.FirstPartyRequiresInstall == nil && im.FirstPartyRequiresInstall != nil {
				em.FirstPartyRequiresInstall = im.FirstPartyRequiresInstall
			}
			if em.ShowInComposerWhenUnlinked == nil && im.ShowInComposerWhenUnlinked != nil {
				em.ShowInComposerWhenUnlinked = im.ShowInComposerWhenUnlinked
			}
		} else {
			existing.AppMetadata = incoming.AppMetadata
		}
	}

	if existing.Labels == nil && incoming.Labels != nil {
		existing.Labels = incoming.Labels
	}
}
