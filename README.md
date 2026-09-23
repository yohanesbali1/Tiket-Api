# Tiket API — Go + Redis

Contoh virtual waiting room seperti sistem antrean tiket:

- Maksimal user aktif: `100`
- User ke-101 dst masuk FIFO queue
- User yang lolos mendapat `access_token`
- User yang masih antre mendapat `queue_token`
- Setiap join menghasilkan `session_id` unik
- Posisi antrean dan estimasi waktu tersedia
- Frontend melakukan polling status
- User aktif wajib heartbeat
- Jika heartbeat berhenti dan lease habis, slot dibebaskan
- User yang `leave` lalu `/join` lagi mendapat posisi baru di belakang
- Redis Lua script dipakai untuk operasi penting yang harus atomic
- Promoter background memindahkan user antrean ke active ketika slot tersedia

## API Endpoints

| Endpoint | Method | Body |
|----------|--------|------|
| `/health` | GET | — |
| `/api/queue/join` | POST | *(kosong)* |
| `/api/queue/status?token=...` | GET | — |
| `/api/queue/heartbeat` | POST | `{"access_token":"..."}` |
| `/api/queue/leave` | POST | `{"token":"..."}` |

## Jalankan dengan Docker

```bash
cp .env.example .env

IMAGE_REPO=OWNER/REPO IMAGE_TAG=latest docker compose up -d
```

Atau build lokal:

```bash
docker build -t ghcr.io/tiket-api:local .
IMAGE_REPO=tiket-api IMAGE_TAG=local docker compose up -d
```

API:
- http://localhost:8080/health
- http://localhost:8080/api/queue/join
- http://localhost:8080/api/queue/status
- http://localhost:8080/api/queue/heartbeat
- http://localhost:8080/api/queue/leave

## Test

Buat 100 user pertama:

```bash
for i in $(seq 1 100); do
  curl -s -X POST http://localhost:8080/api/queue/join
  echo
done
```

User ke-101:

```bash
curl -s -X POST http://localhost:8080/api/queue/join
```

Contoh response:

```json
{
  "status": "waiting",
  "session_id": "sess_xxxxxxxxx",
  "queue_token": "q_xxxxxxxxx",
  "position": 1,
  "estimated_wait_seconds": 300,
  "estimated_wait": "5 minutes"
}
```

Simpan `session_id` untuk identifikasi session.

Cek status:

```bash
curl -s "http://localhost:8080/api/queue/status?token=q_xxxxxxxxx"
```

Ketika sudah lolos:

```json
{
  "status": "allowed",
  "session_id": "sess_xxxxxxxxx",
  "access_token": "acc_xxxxxxxxx",
  "expires_in": 59
}
```

## Heartbeat

Setelah user mendapat `access_token`, frontend kirim heartbeat setiap 10–15 detik:

```bash
curl -s -X POST http://localhost:8080/api/queue/heartbeat \
  -H "Content-Type: application/json" \
  -d '{"access_token":"acc_xxxxxxxxx"}'
```

Lease default 60 detik. Jika heartbeat berhenti, session akan expired dan slot diberikan ke antrean berikutnya.

## Leave

```bash
curl -s -X POST http://localhost:8080/api/queue/leave \
  -H "Content-Type: application/json" \
  -d '{"token":"q_xxxxxxxxx"}'
```

Setelah leave, `/join` lagi membuat session baru dan masuk ke belakang antrean.

## Perilaku Refresh Browser

Sistem berbasis `session_id` yang di-generate server:

- Setiap request `/join` membuat `session_id` baru → user masuk di posisi terakhir antrean
- Refresh browser = keluar dari antrean + join ulang di posisi terakhir
- Tidak ada header identitas yang perlu dikirim client

Contoh:
- User join → `session_id: sess_abc`, posisi antrean #5
- User refresh browser (join lagi) → `session_id: sess_xyz` baru, posisi antrean #101 (di belakang)

## Cara mengintegrasikan ke website

Jangan hanya melindungi halaman `/queue`. Endpoint bisnis juga harus memvalidasi access token.

Contoh:

```text
Next.js
  |
  +-- /queue/join       -> Go Queue
  +-- /queue/status     -> Go Queue
  +-- /queue/heartbeat  -> Go Queue
  |
  +-- /checkout         -> Go Queue /validate
                           |
                           +-- valid -> Laravel
                           +-- invalid -> 403
```

Untuk production, tambahkan endpoint:

```text
POST /api/queue/validate
```

yang memvalidasi token active. Token jangan hanya dicek di frontend, karena user bisa memanggil API langsung.

## CI/CD

Dua workflow GitHub Actions:

| Workflow | Trigger | Action |
|----------|---------|--------|
| `staging.yml` | Push ke `main` | lint → test → build → push GHCR → SSH deploy staging |
| `production.yml` | Push tag `v*` | lint → test → build → push GHCR → SSH deploy production |

### Setup

GitHub Settings → Secrets and variables → Actions:

| Secret | Value |
|--------|-------|
| `SSH_KEY` | Private SSH key untuk server |
| `SSH_HOST` | `user@server-ip` |

### Deploy

```bash
# Staging — cukup push
git push origin main

# Production — harus buat tag
git tag v1.0.0
git push origin v1.0.0
```

## Penting untuk production

1. Gunakan HTTPS.
2. Access token sebaiknya opaque/random seperti contoh ini atau JWT jika memang diperlukan.
3. Lindungi endpoint `/join` dengan rate limit.
4. Jangan mengandalkan browser `beforeunload` untuk mendeteksi user keluar. Heartbeat + lease lebih reliable.
5. Untuk banyak instance Go, semua state queue tetap di Redis; jangan simpan state queue di RAM aplikasi.
6. Estimasi waktu hanyalah perkiraan. Untuk event nyata, hitung throughput aktual dan tampilkan sebagai estimasi.
7. Untuk ticket sale, tambahkan reservation/checkout timeout terpisah dari waiting-room lease.
8. Kalau traffic sangat besar, letakkan CDN/WAF/load balancer di depan queue service.

## State

Redis menggunakan:

```text
vq:active               sorted set: token -> expiry timestamp
vq:waiting              sorted set: token -> FIFO sequence
vq:sequence             counter
vq:session:{sessionID}  session -> current queue/access token
vq:data:{token}         hash: status/session_id/created_at/expires_at/sequence
```

### Kenapa sorted set?

Karena kita perlu:

- FIFO queue dengan `ZRANGE`
- posisi user dengan `ZRANK`
- expiration active session dengan score timestamp
- menghapus session yang expired dengan `ZRANGEBYSCORE`

### Alur user keluar

```text
ACTIVE
  |
  | heartbeat berhenti
  v
LEASE EXPIRED
  |
  v
remove active
  |
  v
promote waiting[0]
  |
  v
waiting user mendapat access token
```

### Refresh browser

Refresh browser membuat user **keluar dari antrean dan join ulang di belakang** (posisi baru). Session lama dihapus, user mendapat `session_id` dan `queue_token` baru dengan posisi di akhir antrean.

Untuk production: simpan `access_token` dalam HttpOnly Secure cookie, gunakan cookie untuk heartbeat. Jika user refresh sambil `allowed`, mereka tetap `allowed` (heartbeat valid). Jika `waiting`, refresh = join ulang di belakang.

### Security note

Contoh ini fokus ke mekanisme queue. Untuk production, token harus divalidasi di semua endpoint sensitif, bukan hanya di frontend.
