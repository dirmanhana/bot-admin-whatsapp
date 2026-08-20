@echo off
REM ============================================================
REM  Build bot-admin-whatsapp.exe (Windows)
REM  Jalankan: build.bat  (butuh Go terpasang: https://go.dev/dl)
REM  Catatan: aplikasi ini butuh PostgreSQL — pastikan DATABASE_URL
REM  di .env menunjuk ke server PostgreSQL yang berjalan.
REM ============================================================
setlocal

echo Building bot-admin-whatsapp.exe ...
go build -o bot-admin-whatsapp.exe .
if errorlevel 1 (
    echo BUILD GAGAL!
    exit /b 1
)
echo.
echo OK. File: bot-admin-whatsapp.exe
echo Jalankan dengan: bot-admin-whatsapp.exe
echo (pastikan file .env ada di folder yang sama, lihat .env.example)
endlocal
