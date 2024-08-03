import JSConfetti from '@/utils/confetti/index';

let confetti: JSConfetti | null = null;
let confettiTimeout: number | null = null;

export default function makeConfetti() {
  if (!confetti) {
    confetti = new JSConfetti();
  }
  for (let i = 0; i < 300; i += 100) {
    setTimeout(() => {
      confetti!.addConfetti({
        confettiNumber: 80
      });
    }, i);
  }
  setTimeout(() => {
    confetti!.addConfetti({
      emojis: ['🟢', '💚', '🗹', '🮱', '✅', '😃', '👑'],
      emojiColors: ['#74fc62', '#47e332', '#8dc785', '#258a17'],
      emojiSize: 30,
      confettiNumber: 80
    });
  }, 400);
  if (confettiTimeout) {
    clearTimeout(confettiTimeout);
  }
  confettiTimeout = setTimeout(() => {
    confetti?.destroyCanvas();
    confetti = null;
  }, 10000);
}
