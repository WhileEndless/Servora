import { computed, ref, watch } from "vue";

const messages = {
  en: {
    overview: "Overview", processes: "Processes", services: "Services", packages: "Packages",
    network: "Network", schedules: "Schedules", alerts: "Alerts",
    activity: "Activity", settings: "Settings", search: "Search current view…",
    signIn: "Sign in to your host", username: "Username", password: "Password",
    signInButton: "Sign in securely", pamHint: "Use an authorized Linux account. Credentials are verified by PAM.",
    uptime: "Uptime", updated: "Updated", performance: "Performance history",
    topProcesses: "Top processes", systemHealth: "System health", logout: "Sign out",
  },
  tr: {
    overview: "Genel Bakış", processes: "İşlemler", services: "Servisler", packages: "Paketler",
    network: "Ağ", schedules: "Zamanlayıcılar", alerts: "Alarmlar",
    activity: "Aktiviteler", settings: "Ayarlar", search: "Geçerli görünümde ara…",
    signIn: "Sunucunuza giriş yapın", username: "Kullanıcı adı", password: "Parola",
    signInButton: "Güvenli giriş", pamHint: "Yetkili bir Linux hesabı kullanın. Bilgiler PAM tarafından doğrulanır.",
    uptime: "Çalışma süresi", updated: "Güncellendi", performance: "Performans geçmişi",
    topProcesses: "En yoğun işlemler", systemHealth: "Sistem sağlığı", logout: "Çıkış yap",
  },
} as const;

export type Locale = keyof typeof messages;
export type MessageKey = keyof typeof messages.en;

const saved = localStorage.getItem("sms.locale");
const initial: Locale = saved === "tr" || (!saved && navigator.language.startsWith("tr")) ? "tr" : "en";
const locale = ref<Locale>(initial);
document.documentElement.lang = initial;

export type TranslationValues = Record<string, string | number>;

function interpolate(message: string, values?: TranslationValues): string {
  if (!values) return message;
  return message.replace(/\{(\w+)\}/g, (match, key: string) => String(values[key] ?? match));
}

export function useI18n() {
  const setLocale = (value: Locale): void => {
    locale.value = value;
    localStorage.setItem("sms.locale", value);
    document.documentElement.lang = value;
  };
  const t = (key: MessageKey): string => messages[locale.value][key];
  const l = (english: string, turkish: string, values?: TranslationValues): string =>
    interpolate(locale.value === "tr" ? turkish : english, values);
  return { locale, setLocale, t, l, messages: computed(() => messages[locale.value]) };
}

watch(locale, (value) => { document.documentElement.lang = value; });
