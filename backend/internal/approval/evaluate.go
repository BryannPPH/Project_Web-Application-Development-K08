package approval

// evaluate menghitung status: tanpa group atau ada group tanpa leader -> blocked;
// ada penolakan -> rejected; semua leader setuju -> approved; selain itu pending.
func evaluate(groups []RequiredGroup, decisions map[string]DecisionRecord) (Status, []GroupReview, int) {
	reviews := make([]GroupReview, 0, len(groups))
	approved := 0
	missingLeader := false
	rejected := false

	for _, group := range groups {
		review := GroupReview{RequiredGroup: group}

		if group.Leader == nil {
			missingLeader = true
			reviews = append(reviews, review)
			continue
		}

		if record, ok := decisions[group.Leader.UserID]; ok {
			decision := record.Decision
			decidedAt := record.DecidedAt
			review.Decision = &decision
			review.Note = record.Note
			review.DecidedAt = &decidedAt

			switch decision {
			case DecisionApproved:
				approved++
			case DecisionRejected:
				rejected = true
			}
		}
		reviews = append(reviews, review)
	}

	switch {
	case len(groups) == 0 || missingLeader:
		return StatusBlocked, reviews, approved
	case rejected:
		return StatusRejected, reviews, approved
	case approved == len(groups):
		return StatusApproved, reviews, approved
	default:
		return StatusPending, reviews, approved
	}
}

// isRequiredLeader: apakah userID leader dari minimal satu group wajib.
func isRequiredLeader(groups []RequiredGroup, userID string) bool {
	for _, group := range groups {
		if group.Leader != nil && group.Leader.UserID == userID {
			return true
		}
	}
	return false
}
