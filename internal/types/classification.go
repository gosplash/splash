package types

type Classification int

const (
	ClassPublic     Classification = iota
	ClassInternal
	ClassSensitive
	ClassRestricted
)

func (c Classification) String() string {
	switch c {
	case ClassPublic:
		return "public"
	case ClassInternal:
		return "internal"
	case ClassSensitive:
		return "sensitive"
	case ClassRestricted:
		return "restricted"
	default:
		return "unknown"
	}
}

// LoggableMaxClassification is the maximum classification a type can have
// and still satisfy the Loggable constraint.
const LoggableMaxClassification = ClassInternal
