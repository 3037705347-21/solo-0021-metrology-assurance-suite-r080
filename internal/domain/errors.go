package domain

import "errors"

var (
	ErrInvalidInput         = errors.New("invalid input")
	ErrNotFound             = errors.New("resource not found")
	ErrDuplicateAssetTag    = errors.New("duplicate asset tag")
	ErrDuplicateCertificate = errors.New("duplicate calibration certificate")
	ErrActiveCaseExists     = errors.New("device already has an unfinished case")
	ErrActivePlanExists     = errors.New("device already has an open calibration plan")
	ErrBasisInvalid         = errors.New("calibration basis is missing or invalid")
	ErrStateConflict        = errors.New("resource state conflict")
	ErrSealAlreadyCreated   = errors.New("case already has an evidence seal")
)

func IsKnownError(err error) bool {
	return errors.Is(err, ErrInvalidInput) ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrDuplicateAssetTag) ||
		errors.Is(err, ErrDuplicateCertificate) ||
		errors.Is(err, ErrActiveCaseExists) ||
		errors.Is(err, ErrActivePlanExists) ||
		errors.Is(err, ErrBasisInvalid) ||
		errors.Is(err, ErrStateConflict) ||
		errors.Is(err, ErrSealAlreadyCreated)
}
