package congestion

import (
	"github.com/lucas-clemente/quic-go/congestion"
	"time"
)

const (
	maxDatagramSize = 1252

	ackRateMinSampleInterval = 4 * time.Second
	ackRateMaxSampleInterval = 20 * time.Second
	ackRateMinACKSampleCount = 100
)

type BrutalSender struct {
	rttStats congestion.RTTStats
	bps      congestion.ByteCount
	pacer    *pacer

	ackCount, lossCount  uint64
	ackRate              float64
	ackRateNextUpdateMin time.Time
	ackRateNextUpdateMax time.Time
}

func NewBrutalSender(bps congestion.ByteCount) *BrutalSender {
	bs := &BrutalSender{
		bps: bps,
	}
	bs.pacer = newPacer(func() uint64 {
		return uint64(float64(bs.bps)/bs.getAckRate()) * 5 / 4
	})
	return bs
}

func (b *BrutalSender) SetRTTStats(rttStats congestion.RTTStats) {
	b.rttStats = rttStats
}

func (b *BrutalSender) TimeUntilSend(bytesInFlight congestion.ByteCount) time.Time {
	return b.pacer.TimeUntilSend()
}

func (b *BrutalSender) HasPacingBudget() bool {
	return b.pacer.Budget(time.Now()) >= maxDatagramSize
}

func (b *BrutalSender) CanSend(bytesInFlight congestion.ByteCount) bool {
	return bytesInFlight < b.GetCongestionWindow()
}

func (b *BrutalSender) GetCongestionWindow() congestion.ByteCount {
	rtt := maxDuration(b.rttStats.LatestRTT(), b.rttStats.SmoothedRTT())
	if rtt <= 0 {
		return 10240
	}
	return congestion.ByteCount(float64(b.bps) * rtt.Seconds() / b.getAckRate())
}

func (b *BrutalSender) OnPacketSent(sentTime time.Time, bytesInFlight congestion.ByteCount,
	packetNumber congestion.PacketNumber, bytes congestion.ByteCount, isRetransmittable bool) {
	b.pacer.SentPacket(sentTime, uint64(bytes))
}

func (b *BrutalSender) OnPacketAcked(number congestion.PacketNumber, ackedBytes congestion.ByteCount,
	priorInFlight congestion.ByteCount, eventTime time.Time) {
	b.ackCount += 1
	b.maybeUpdateACKRate()
}

func (b *BrutalSender) OnPacketLost(number congestion.PacketNumber, lostBytes congestion.ByteCount,
	priorInFlight congestion.ByteCount) {
	b.lossCount += 1
	b.maybeUpdateACKRate()
}

func (b *BrutalSender) maybeUpdateACKRate() {
	now := time.Now()
	if !now.After(b.ackRateNextUpdateMin) {
		return
	}
	// Min interval reached
	if b.ackCount >= ackRateMinACKSampleCount {
		b.ackRate = float64(b.ackCount) / float64(b.ackCount+b.lossCount)
		b.ackCount, b.lossCount = 0, 0
		b.ackRateNextUpdateMin = now.Add(ackRateMinSampleInterval)
		b.ackRateNextUpdateMax = now.Add(ackRateMaxSampleInterval)
	} else {
		if now.After(b.ackRateNextUpdateMax) {
			// Max interval reached, still not enough samples, reset
			b.ackCount, b.lossCount = 0, 0
			b.ackRateNextUpdateMin = now.Add(ackRateMinSampleInterval)
			b.ackRateNextUpdateMax = now.Add(ackRateMaxSampleInterval)
		}
	}
}

func (b *BrutalSender) getAckRate() (rate float64) {
	rate = b.ackRate
	if rate <= 0 {
		rate = 1
	} else if rate < 0.5 {
		rate = 0.5
	}
	return
}

func (b *BrutalSender) InSlowStart() bool {
	return false
}

func (b *BrutalSender) InRecovery() bool {
	return false
}

func (b *BrutalSender) MaybeExitSlowStart() {}

func (b *BrutalSender) OnRetransmissionTimeout(packetsRetransmitted bool) {}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
