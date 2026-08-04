package tmlabseedance

const ChannelName = "tmlab-seedance"

const (
	ModelSeedanceV2      = "[V2]seedance-2.0"
	ModelSeedanceMini    = "seedance-2.0-mini"
	ModelSeedanceFast    = "seedance-2.0-fast"
	ModelSeedancePro     = "seedance-2.0-pro"
	ModelSeedancePro720P = "seedance-2.0-pro-720p"
	ModelSeedanceFast431 = "seedance-2.0-fast(431)"
	ModelSeedancePro431  = "seedance-2.0-pro(431)"
	ModelSeedance25      = "seedance-2.5"
)

var ModelList = []string{
	ModelSeedanceV2,
	ModelSeedanceMini,
	ModelSeedanceFast,
	ModelSeedancePro,
	ModelSeedancePro720P,
	ModelSeedanceFast431,
	ModelSeedancePro431,
	ModelSeedance25,
}

type requestFormat int

const (
	requestFormatStable requestFormat = iota
	requestFormatV2
	requestFormatPro720P
	requestFormat431
	requestFormat25
)

type modelProfile struct {
	format            requestFormat
	promptMin         int
	promptMax         int
	durationMin       int
	durationMax       int
	allowedDurations  map[int]struct{}
	defaultRatio      string
	allowedRatios     map[string]struct{}
	defaultResolution string
	resolutionRatios  map[string]float64
	fixedPrice        bool
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

var modelProfiles = map[string]modelProfile{
	ModelSeedanceV2: {
		format:            requestFormatV2,
		durationMin:       4,
		durationMax:       15,
		defaultRatio:      "16:9",
		allowedRatios:     stringSet("16:9", "9:16"),
		defaultResolution: "720P",
		resolutionRatios:  map[string]float64{"720P": 1, "1080P": 1.0 / 0.75},
	},
	ModelSeedanceMini: stableProfile(),
	ModelSeedanceFast: stableProfile(),
	ModelSeedancePro:  stableProfile(),
	ModelSeedancePro720P: {
		format:            requestFormatPro720P,
		promptMin:         10,
		promptMax:         10000,
		durationMin:       5,
		durationMax:       15,
		defaultRatio:      "9:16",
		allowedRatios:     stringSet("9:16"),
		defaultResolution: "720p",
		resolutionRatios:  map[string]float64{"720p": 1},
	},
	ModelSeedanceFast431: {
		format:            requestFormat431,
		promptMax:         5000,
		allowedDurations:  map[int]struct{}{10: {}, 15: {}},
		defaultRatio:      "16:9",
		allowedRatios:     stringSet("16:9", "9:16", "1:1"),
		defaultResolution: "720p",
		resolutionRatios:  map[string]float64{"720p": 1},
		fixedPrice:        true,
	},
	ModelSeedancePro431: {
		format:            requestFormat431,
		promptMax:         5000,
		durationMin:       4,
		durationMax:       15,
		defaultRatio:      "16:9",
		allowedRatios:     stringSet("16:9", "9:16", "1:1"),
		defaultResolution: "720p",
		resolutionRatios:  map[string]float64{"720p": 1},
		fixedPrice:        true,
	},
	ModelSeedance25: {
		format:            requestFormat25,
		durationMin:       1,
		defaultRatio:      "16:9",
		allowedRatios:     stringSet("16:9", "9:16", "4:3", "3:4"),
		defaultResolution: "720p",
		resolutionRatios:  map[string]float64{"480p": 1, "720p": 1.7 / 0.8},
	},
}

func stableProfile() modelProfile {
	return modelProfile{
		format:            requestFormatStable,
		promptMax:         3000,
		durationMin:       4,
		durationMax:       15,
		defaultRatio:      "adaptive",
		allowedRatios:     stringSet("adaptive", "16:9", "4:3", "1:1", "3:4", "9:16", "21:9"),
		defaultResolution: "720p",
		resolutionRatios:  map[string]float64{"480p": 1, "720p": 2},
	}
}
