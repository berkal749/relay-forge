package execution

import "time"

// RetryDelay returns exponential backoff for the given attempt number.
func RetryDelay(attempt int32, base, maximum time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if base <= 0 {
		return 0
	}

	delay := base
	for i := int32(1); i < attempt; i++ {
		if delay >= maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}
