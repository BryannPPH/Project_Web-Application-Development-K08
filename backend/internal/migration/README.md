# Migration module integration

`NewModule` membutuhkan dua adapter dari modul database/access:

- `Authorizer` harus menerapkan pemeriksaan view dan edit berdasarkan grant grup.
  Platform admin tidak mendapat bypass implisit.
- `TargetProvider` membuka koneksi PostgreSQL target berdasarkan `databaseID`.
  Modul migration tidak membaca atau mendekripsi credential.

Daftarkan `handler.RegisterRoutes` dari `app.WithFeatureFactory`. Handler sudah
memasang `RequireAuthenticated` pada GET dan `ProtectMutation` pada POST/PATCH.

Konfigurasi yang disarankan:

```go
migration.ServiceConfig{
    MaxSQLBytes: 256 * 1024,
    ExecutionTimeout: 2 * time.Minute,
}
```

Eksekusi memegang session advisory lock pada platform database dari sebelum
state diperiksa ulang sampai status terminal disimpan. SQL dijalankan dengan
simple protocol di dalam satu target transaction, sehingga multi-statement tetap
atomic. Transaction-control SQL ditolak oleh scanner sebelum draft disimpan dan
diperiksa lagi tepat sebelum eksekusi.

Entry `interrupted` tidak pernah dicoba ulang otomatis. Operasional harus
memeriksa keadaan target, mencatat hasil rekonsiliasi, lalu memperbaiki status
melalui prosedur administratif yang diaudit. `Repository.RecoverRunning` hanya
disediakan sebagai primitive recovery eksplisit; jangan panggil otomatis saat
startup.

Status HTTP utama: `201` create, `200` read/update/execute, `400` validasi,
`401` tanpa session, `403` tanpa grant/CSRF, `404` tidak ditemukan, `409`
konflik state/order, dan `422` pelanggaran kebijakan SQL.

