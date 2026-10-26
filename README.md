# taktaplus-Lic

سرور لایسنس و مخزن مرکزی پکیج‌های [TaktaPlus](https://github.com/farshadfighter/taktaplus).
معماری: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)

## اجزا

| مسیر | توضیح |
|---|---|
| `cmd/licserver` | سرور لایسنس (API + پنل وب) |
| `cmd/lictool` | ابزار خط فرمان: ساخت کلید، امضا و بررسی لایسنس و Manifest |
| `pkg/keys` | فرمت کلید عمومی و Keyring |
| `pkg/licformat` | فرمت فایل لایسنس (`.tplic`) و درخواست فعال‌سازی (`.tpreq`) — مشترک با TaktaPlus |
| `pkg/manifest` | فرمت Manifest امضاشده‌ی پکیج‌ها — مشترک با TaktaPlus |
| `web/` | پنل وب (React + Ant Design، راست‌به‌چپ) |

## توسعه

پیش‌نیاز: Go 1.26+، Node.js 22+، Docker.

```sh
cp .env.example .env
make db-up      # PostgreSQL روی 127.0.0.1:5433
make web        # ساخت رابط وب (یک‌بار، یا بعد از تغییر web/)
# TPLIC_MASTER_KEY را در .env پر کنید: head -c32 /dev/urandom | base64
make run        # http://localhost:8080
./bin/licserver create-admin -email you@example.com   # ساخت اولین کاربر (Owner)
make test lint
```

برای رابط وب با Hot Reload: `cd web && npm run dev` (درخواست‌های `/api` به پورت 8080 فرستاده می‌شوند).

## تنظیمات سرور

| متغیر | توضیح |
|---|---|
| `TPLIC_DATABASE_URL` | اتصال PostgreSQL (الزامی) |
| `TPLIC_MASTER_KEY` | ۳۲ بایت تصادفی base64 برای رمزنگاری Secretها (الزامی) |
| `TPLIC_PUBLIC_URL` | آدرس عمومی سرور؛ در لایسنس‌های آنلاین برای Heartbeat نوشته می‌شود |
| `TPLIC_LICENSE_KEY_FILE` / `TPLIC_LICENSE_KEY_PASSPHRASE` | کلید امضای لایسنس (ساخته‌شده با `lictool keygen`)؛ بدون آن صدور لایسنس غیرفعال است |
| `TPLIC_COOKIE_SECURE` | فقط برای توسعه‌ی محلی روی HTTP مقدار `false` |

## API نصب‌ها

| مسیر | توضیح |
|---|---|
| `POST /api/v1/activate` | `{"request": "TPREQ1…"}` ← `{"license": "TPLIC1…", "license_id": "LIC-…"}` |
| `POST /api/v1/heartbeat` | درخواست refresh ← `{"status": "updated" \| "unchanged"}` یا `410` با `revoked` |

## کلیدها

کلید خصوصی **هرگز** وارد ریپازیتوری نمی‌شود (`*.key` در `.gitignore` است).

```sh
export TPLIC_KEY_PASSPHRASE='…'                 # حداقل ۱۲ کاراکتر
lictool keygen --kid lic-2026-01 --out /secure/keys
lictool sign-license --key /secure/keys/lic-2026-01.key --in license.json --request install.tpreq --out license.tplic
lictool verify-license --pub /secure/keys/lic-2026-01.pub --in license.tplic
lictool sign-manifest --key /secure/keys/pkg-2026-01.key --in manifest.json --out manifest.sig
```

فایل `.pub` (کلید عمومی) را می‌توان در برنامه‌ی TaktaPlus قرار داد.
