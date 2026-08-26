let ctx: AudioContext | null = null;

function audioCtx(): AudioContext | null {
  if (typeof window === "undefined") return null;
  if (!ctx) {
    const ACtor =
      window.AudioContext ??
      (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!ACtor) return null;
    ctx = new ACtor();
  }
  return ctx;
}

export function playDing() {
  const ac = audioCtx();
  if (!ac) return;
  if (ac.state === "suspended") void ac.resume();
  const now = ac.currentTime;

  const strike = (freq: number, gain: number, delay: number, dur: number) => {
    const osc = ac.createOscillator();
    const g = ac.createGain();
    osc.type = "sine";
    osc.frequency.value = freq;
    g.gain.setValueAtTime(0.0001, now + delay);
    g.gain.exponentialRampToValueAtTime(gain, now + delay + 0.012);
    g.gain.exponentialRampToValueAtTime(0.0001, now + delay + dur);
    osc.connect(g);
    g.connect(ac.destination);
    osc.start(now + delay);
    osc.stop(now + delay + dur + 0.05);
  };

  strike(880, 0.12, 0, 0.6);
  strike(1318.51, 0.07, 0.09, 0.5);
}
