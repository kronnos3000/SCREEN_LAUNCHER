//go:build windows

package main

// Variant is set at build time: "CR2" (Conference Room 2) or "SD" (Sales Display).
// Build with: go build -ldflags "-X main.Variant=SD -X main.Version=1.0.0"
var Variant = "CR2" // default to CR2 if not set
var Version = "dev" // set at build time

// VariantInfo describes a launcher variant.
type VariantInfo struct {
	Name        string // short code: CR2, SD
	Title       string // window title
	Description string // for GitHub release naming
	Screens     int    // expected monitor count (1 or 2)
	MeetMinimized bool // open Meet minimized (for recording)
}

var Variants = map[string]VariantInfo{
	"CR2": {
		Name:        "CR2",
		Title:       "Construction Corps Meeting Launcher",
		Description: "Conference Room 2 (dual screen, Weekly Gantt)",
		Screens:     2,
		MeetMinimized: false,
	},
	"SD": {
		Name:        "SD",
		Title:       "Sales Display Launcher",
		Description: "Sales Display (single screen, Sales Pipeline)",
		Screens:     1,
		MeetMinimized: true, // Meet stays minimized for recording
	},
}

func CurrentVariant() VariantInfo {
	if v, ok := Variants[Variant]; ok {
		return v
	}
	return Variants["CR2"]
}

// defaultConfigForVariant returns the preset configuration for each variant.
func defaultConfigForVariant(v string) config {
	switch v {
	case "SD":
		return sdConfig()
	default:
		return cr2Config()
	}
}

// CR2: Conference Room 2 - dual screen, Weekly Gantt meeting
func cr2Config() config {
	m := func(name, meet string, other ...string) meeting {
		return meeting{Name: name, MeetURL: meet, MeetScreen: "left", OtherScreen: "right", OtherLinks: other}
	}
	return config{
		Gantt: m(ganttGroup, "https://ccorps.net/productiongooglemeet"),
		Meetings: []meeting{
			m("In-office meeting", "https://ccorps.net/in-officegooglemeet", "https://ccorps.net/in-officemeetingnotes"),
			m("Company-wide meeting", "https://ccorps.net/companywidemeeting"),
			m("Field team meeting", "https://ccorps.net/fieldteamgooglemeet"),
		},
		JobTread: defaultJT(),
	}
}

// SD: Sales Display - single screen, Sales Pipeline views
func sdConfig() config {
	return config{
		SalesDisplay: &sdSettings{
			MeetURL:       "https://ccorps.net/companywidemeeting",
			MeetMinimized: true,
			ListViewID:    "22PheCYEmTBN", // SALES PIPELINE (List View)
			KanbanViewID:  "22PggTBEEDCM", // SALES PIPELINE (Kanban)
		},
		JobTread: defaultJT(),
	}
}

// sdSettings holds Sales Display specific configuration.
type sdSettings struct {
	MeetURL       string `json:"meetUrl"`
	MeetMinimized bool   `json:"meetMinimized"`
	ListViewID    string `json:"listViewId"`    // SALES PIPELINE (List View)
	KanbanViewID  string `json:"kanbanViewId"`  // SALES PIPELINE (Kanban)
}
