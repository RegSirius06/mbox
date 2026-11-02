package main

func peekDownBatchLocked(s *session, limit int) ([]byte, int, int) {
	if len(s.downFrames) == 0 {
		return nil, 0, 0
	}
	if limit <= 0 {
		limit = 256 * 1024
	}
	total, count := 0, 0
	for count < len(s.downFrames) {
		n := len(s.downFrames[count])
		if count > 0 && total+n > limit {
			break
		}
		total += n
		count++
		if total >= limit {
			break
		}
	}
	data := make([]byte, 0, total)
	for i := 0; i < count; i++ {
		data = append(data, s.downFrames[i]...)
	}
	return data, count, total
}

func consumeDownBatchLocked(s *session, count, total int) {
	if count <= 0 {
		return
	}
	for i := 0; i < count; i++ {
		s.downFrames[i] = nil
	}
	s.downFrames = s.downFrames[count:]
	s.downBytes -= total
	if s.downBytes < 0 {
		panic("negative DOWN queue size")
	}
	releaseGlobalQueued(total)
	if len(s.downFrames) == 0 {
		s.downFrames = nil
		select {
		case <-s.downReady:
		default:
		}
	}
}

func popDownBatchLocked(s *session, limit int) []byte {
	data, count, total := peekDownBatchLocked(s, limit)
	consumeDownBatchLocked(s, count, total)
	return data
}
