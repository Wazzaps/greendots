import { ref } from 'vue';

export const negative_emojis = [
  '🤔',
  '😅',
  '🥲',
  '🤨',
  '😕',
  '🙃',
  '🧐',
  '😐',
  '😑',
  '😬',
  '🫠',
  '😯',
  '😦',
  '😧',
  '😮',
  '😲',
  '😵‍💫',
  '💩',
  '💀',
  '🙈',
  '🙉',
  '🙊',
  '🐞',
  '🚧',
  '🗿',
  '🪦',
  '📉',
  '💔',
  '👀',
  '⛈',
  '🌩'
];

export const positive_emojis = [
  '😀',
  '😃',
  '😄',
  '😁',
  '😊',
  '😇',
  '🙂',
  '😌',
  '😍',
  '😋',
  '🤓',
  '😎',
  '🤩',
  '🥳',
  '🤑',
  '🤠',
  '🤘',
  '💪',
  '🎩',
  '🐧',
  '🐤',
  '🦆',
  '🦄',
  '🐢',
  '🦀',
  '🐳',
  '🌼',
  '🌞',
  '⭐️',
  '🌟',
  '✨',
  '🔥',
  '🌈',
  '☀️',
  '🎂',
  '🍰',
  '🍻',
  '🪂',
  '🏆',
  '🥇',
  '🎖',
  '🥁',
  '🪇',
  '🎺',
  '🎸',
  '🎯',
  '✈️',
  '🚀',
  '🗽',
  '🏖',
  '🌠',
  '💎',
  '🎁',
  '🎊',
  '🎉',
  '🪩',
  '📈',
  '❤️',
  '💯'
];

export const notifications_enabled = ref(
  'Notification' in window &&
    localStorage.getItem('notificationsEnabled') === 'true' &&
    Notification.permission === 'granted'
);

export function enableNotififcations() {
  const requestedAt = new Date();
  Notification.requestPermission()
    .then((result) => {
      if (result === 'granted') {
        notifications_enabled.value = true;
        localStorage.setItem('notificationsEnabled', 'true');
      } else if (requestedAt.getTime() + 100 > new Date().getTime()) {
        alert(
          'Notifications were blocked, please enable them using the site menu button in left side of the address bar.'
        );
      }
    })
    .catch((error) => {
      console.error(error);
    });
}

export function disableNotifications() {
  notifications_enabled.value = false;
  localStorage.setItem('notificationsEnabled', 'false');
}

export function toggleNotifications() {
  if (notifications_enabled.value) {
    disableNotifications();
  } else {
    enableNotififcations();
  }
}

export function notify(title: string, options?: NotificationOptions) {
  if (notifications_enabled.value) {
    const notification = new Notification(title, options);
    notification.addEventListener('click', () => {
      window.focus();
      notification.close();
    });
  }
}
