# taktaplus-Lic

سرور لایسنس و مخزن مرکزی پکیج‌های [TaktaPlus](https://github.com/farshadfighter/taktaplus).
معماری: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)

## پنل‌ها

- **ادمین شرکت:** مشتریان، نمایندگان و اعتبار پیش‌خرید آن‌ها، صدور و تمدید لایسنس، فعال‌سازی آفلاین، نصب‌ها، مخزن پکیج و کاربران پورتال مشتریان و نمایندگان.
- **پورتال مشتری:** مشاهده‌ی لایسنس‌ها، سریال‌ها، مهلت و نصب متصل؛ فعال‌سازی آفلاین (بارگذاری `.tpreq`) و دانلود فایل لایسنس.
- **پورتال نماینده:** مدیریت مشتریان خود و کاربران پورتال آن‌ها؛ صدور، افزودن و تمدید لایسنس از محل اعتبار سریال‌سال و کاربرسال 2FA (کسر اعتبار و تغییر لایسنس در یک تراکنش)؛ فعال‌سازی آفلاین برای مشتریان خود.

- **سیستم (Owner):** بکاپ رمزنگاری‌شده‌ی خود سرور لایسنس (دیتابیس، مخزن، فایل‌های کلید امضا و کلید اصلی) با عبارت عبور جدا، زمان‌بندی و کپی SFTP؛ ارتقا با بسته‌ی آفلاین امضاشده و برگشت خودکار در صورت خطا.

- **API (Owner):** توکن‌های API برای سامانه‌های دیگر (نقش‌های کارکنان شرکت، فقط‌خواندنی، انقضا، IP مجاز) در «سیستم ← توکن‌های API» و مستند OpenAPI در `/api/v1/openapi.json` و صفحه‌ی «مستند API»؛ مدیریت کاربران و توکن‌ها، ارتقا و عبارت عبور بکاپ با توکن ممکن نیست.

کاربران پورتال با رمز اولیه‌ای که ادمین یا نماینده تعیین می‌کند وارد می‌شوند و در اولین ورود ورود دو مرحله‌ای (TOTP) را فعال می‌کنند.

## اجزا

| مسیر | توضیح |
|---|---|
| `cmd/licserver` | سرور لایسنس (API + پنل وب) |
| `cmd/lictool` | ابزار خط فرمان: ساخت کلید، امضا و بررسی لایسنس و Manifest |
| `pkg/keys` | فرمت کلید عمومی و Keyring |
| `pkg/licformat` | فرمت فایل لایسنس (`.tplic`) و درخواست فعال‌سازی (`.tpreq`) — مشترک با TaktaPlus |
| `pkg/manifest` | فرمت Manifest امضاشده‌ی پکیج‌ها — مشترک با TaktaPlus |
| `pkg/release` | بسته‌ی نصب/ارتقای آفلاین امضاشده (`lictool build-release` / `verify-release`) — برای هر دو محصول |
| `pkg/selfbackup`, `pkg/sysadmin` | بکاپ رمزنگاری‌شده (Argon2id + XChaCha20-Poly1305)، SFTP و پیاده‌سازی مشترک بکاپ و ارتقای خود محصول |
| `pkg/apitoken`, `pkg/apidoc` | قالب توکن API (`tpk_…`، فقط Hash ذخیره می‌شود) و ساخت مستند OpenAPI — مشترک با TaktaPlus |
| `internal/releasekeys/keys` | کلیدهای **عمومی** انتشار که داخل باینری قرار می‌گیرند |
| `deploy/bundle`, `scripts/release` | فایل‌های بسته‌ی نصب و اسکریپت ساخت آن |
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
| `TPLIC_PACKAGE_KEY_FILE` / `TPLIC_PACKAGE_KEY_PASSPHRASE` | کلید امضای Manifest مخزن پکیج (`lictool keygen -kid pkg-...`)؛ جدا از کلید لایسنس |
| `TPLIC_DATA_DIR` | محل فایل‌های پکیج (`repo/`)، پیش‌فرض `/var/lib/taktaplus-lic` |
| `TPLIC_COOKIE_SECURE` | فقط برای توسعه‌ی محلی روی HTTP مقدار `false` |
| `TPLIC_PG_DUMP` / `TPLIC_PG_RESTORE` | ابزار PostgreSQL برای بکاپ خود سرور، پیش‌فرض `pg_dump` و `pg_restore` (در ایمیج نصب است) |

## نصب و ارتقا (بسته‌ی آفلاین)

```sh
tar xzf taktaplus-lic-1.2.0.tar.gz -C /tmp/lic && cd /tmp/lic
sudo ./install.sh https://license.example.com    # آدرس عمومی؛ یک Reverse Proxy با TLS جلوی پورت 8080 قرار دهید
```

سپس کلیدهای امضای رمزنگاری‌شده را در `/opt/taktaplus-lic/data/keys/` بگذارید، مسیر (`/var/lib/taktaplus-lic/keys/<kid>.key`) و عبارت عبورشان را در `/opt/taktaplus-lic/.env` وارد و `docker compose up -d` کنید؛ اولین Owner: `docker compose exec licserver licserver create-admin -email you@example.com`.

- **ارتقا:** از پنل («سیستم ← بکاپ و ارتقای سیستم») یا `sudo /opt/taktaplus-lic/scripts/upgrade.sh taktaplus-lic-1.3.0.tar.gz`
- **بکاپ دستی:** `sudo /opt/taktaplus-lic/scripts/backup.sh`؛ **بازیابی:** `sudo /opt/taktaplus-lic/scripts/restore.sh FILE.tpbak`
- عبارت عبور کلیدهای امضا در بکاپ نیست؛ پس از بازیابی روی سرور تازه آن‌ها را دوباره در `.env` وارد کنید.
- ساخت بسته: کلید عمومی انتشار را در `internal/releasekeys/keys/` بگذارید و `TPLIC_KEY_PASSPHRASE=... scripts/release/build-bundle.sh 1.3.0 /secure/keys/rel-2026-01.key 1.0.0`.

اسکریپت‌های `deploy/bundle/scripts` بین هر دو محصول یکسان‌اند و فقط `product.sh` فرق دارد.

## API

مستند کامل در `/api/v1/openapi.json`؛ با توکن: `curl -H "Authorization: Bearer tpk_…" https://license.example.com/api/v1/licenses`. پس از تغییر مسیرها در `internal/server/server.go`، `make apidoc` را اجرا کنید.

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
lictool build-release --key /secure/keys/rel-2026-01.key --dir build/ --product taktaplus --version 1.3.0 --out taktaplus-1.3.0.tar.gz
lictool verify-release --pub /secure/keys/rel-2026-01.pub taktaplus-1.3.0.tar.gz
```

فایل `.pub` (کلید عمومی) را می‌توان در برنامه‌ی TaktaPlus قرار داد.
