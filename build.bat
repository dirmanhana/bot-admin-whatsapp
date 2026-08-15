@echo off
REM ============================================================
REM  Build bot-admin-whatsapp.exe (mode SQLite, tanpa CGO)
REM  Jalankan: build.bat  (butuh Go terpasang: https://go.dev/dl)
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
