package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/template/html/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// --- MODELS ---
type Akun struct {
	ID       uint   `gorm:"primaryKey"`
	NoWA     string `gorm:"unique"`
	Password string
	Role     string `gorm:"default:'Wali'"`
}

type Siswa struct {
	ID           uint `gorm:"primaryKey"`
	AkunID       uint
	Nama         string
	JenisKelamin string
	Program      string
	FileKK       string
	FileAkta     string
	FileKTPWali  string
	FileIjazah   string
	Status       string `gorm:"default:'Menunggu Verifikasi'"`
	Kamar        string `gorm:"default:'Belum Diplot'"`
}

type JurnalTahfidz struct {
	ID      uint   `gorm:"primaryKey"`
	SiswaID uint   `form:"siswa_id"`
	Juz     string `form:"juz"`
	Surat   string `form:"surat"`
	Tanggal time.Time
}

type PembayaranSPP struct {
	ID      uint `gorm:"primaryKey"`
	SiswaID uint
	Bulan   string
	Tahun   int
	Jumlah  int
	Status  string `gorm:"default:'Belum Lunas'"`
}

type Pengaturan struct {
	ID    uint   `gorm:"primaryKey"`
	Key   string `gorm:"unique"`
	Value string
}

type Notifikasi struct {
	ID       uint `gorm:"primaryKey"`
	AkunID   uint
	Kategori string
	Judul    string
	Pesan    string
	Tanggal  time.Time
}

type KPIAnak struct {
	DataSiswa    Siswa
	HafalanAkhir string
	TagihanAktif bool
}

// --- SETUP DB & SESSION ---
var DB *gorm.DB
var store *session.Store

func connectDB() {
	dsn := "root:@tcp(127.0.0.1:3306)/db_pendaftaran?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("DB Error:", err)
	}
	db.AutoMigrate(&Akun{}, &Siswa{}, &JurnalTahfidz{}, &PembayaranSPP{}, &Pengaturan{}, &Notifikasi{})

	// Inject Data Saklar PPDB dengan FirstOrCreate (Anti Error Duplicate)
	var setting Pengaturan
	db.Where(Pengaturan{Key: "STATUS_PPDB"}).FirstOrCreate(&setting, Pengaturan{Value: "BUKA"})

	DB = db
	fmt.Println("🚀 DATABASE AR-ROMLAH READY!")
}

// --- WHATSAPP & INBOX ---
func kirimNotif(akunID uint, noWA string, kategori string, judul string, pesan string) {
	DB.Create(&Notifikasi{AkunID: akunID, Kategori: kategori, Judul: judul, Pesan: pesan, Tanggal: time.Now()})

	apiUrl := "https://api.fonnte.com/send"
	token := "TOKEN_API_LU_DISINI"
	payload := map[string]interface{}{"target": noWA, "message": pesan}
	jsonPayload, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", apiUrl, bytes.NewBuffer(jsonPayload))
	req.Header.Add("Authorization", token)
	req.Header.Add("Content-Type", "application/json")
	client := &http.Client{}
	res, err := client.Do(req)
	if err == nil {
		res.Body.Close()
	}
}

// --- MIDDLEWARES ---
func NoCache(c *fiber.Ctx) error {
	c.Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
	return c.Next()
}
func MustLogin(c *fiber.Ctx) error {
	sess, _ := store.Get(c)
	if sess.Get("akun_id") == nil {
		return c.Redirect("/")
	}
	return c.Next()
}
func MustAdmin(c *fiber.Ctx) error {
	sess, _ := store.Get(c)
	if sess.Get("role") != "Admin" {
		return c.Redirect("/wali/dashboard")
	}
	return c.Next()
}

// --- MAIN ROUTER ---
func main() {
	connectDB()
	// Konfigurasi Session Baja (Anti Mental-Mental Club)
	store = session.New(session.Config{
		Expiration:     24 * time.Hour, // Tahan 24 jam biar ga cepet logout
		CookieHTTPOnly: true,           // Aman dari serangan hacker (XSS)
		CookieSecure:   false,          // Wajib FALSE karena kita tes di lokal (HTTP)
		CookieSameSite: "Lax",          // Biar browser mau nerima cookie dari IP LAN
	})
	// --- SETUP ENGINE HTML ---
	engine := html.New("./views", ".html")

	// TAMBAHIN BARIS INI BRE! Biar ga usah restart server pas ngedit HTML
	engine.Reload(true)

	app := fiber.New(fiber.Config{Views: engine})

	app.Static("/public", "./public")
	app.Static("/uploads", "./uploads")

	app.Get("/", NoCache, func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		if sess.Get("akun_id") != nil {
			if sess.Get("role") == "Admin" {
				return c.Redirect("/admin/dashboard")
			}
			return c.Redirect("/wali/dashboard")
		}
		return c.Render("login", nil)
	})

	app.Post("/register-akun", func(c *fiber.Ctx) error {
		noWa := c.FormValue("no_wa")
		pass := c.FormValue("password")
		var cek Akun
		DB.Where("no_wa = ?", noWa).First(&cek)
		if cek.ID != 0 {
			return c.Redirect("/?status=exist")
		}
		hash, _ := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
		DB.Create(&Akun{NoWA: noWa, Password: string(hash), Role: "Wali"})
		return c.Redirect("/?status=sukses")
	})

	app.Post("/cek-login", func(c *fiber.Ctx) error {
		var akun Akun
		DB.Where("no_wa = ?", c.FormValue("no_wa")).First(&akun)
		if akun.ID == 0 || bcrypt.CompareHashAndPassword([]byte(akun.Password), []byte(c.FormValue("password"))) != nil {
			return c.Redirect("/?status=gagal")
		}

		sess, _ := store.Get(c)
		sess.Set("akun_id", akun.ID)
		sess.Set("role", akun.Role)

		// INI YANG PALING PENTING BRE! JANGAN SAMPE KETINGGALAN
		if err := sess.Save(); err != nil {
			fmt.Println("Error simpan session:", err)
		}

		if akun.Role == "Admin" {
			return c.Redirect("/admin/dashboard")
		}
		return c.Redirect("/wali/dashboard")
	})

	app.Get("/logout", func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		sess.Destroy()
		return c.Redirect("/")
	})

	// === PORTAL WALI ===
	wali := app.Group("/wali", NoCache, MustLogin)

	wali.Get("/dashboard", func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		var setting Pengaturan
		DB.Where(&Pengaturan{Key: "STATUS_PPDB"}).First(&setting)

		var siswas []Siswa
		DB.Where("akun_id = ? AND status = ?", sess.Get("akun_id"), "Lulus").Find(&siswas)

		var kpiList []KPIAnak
		for _, s := range siswas {
			var kpi KPIAnak
			kpi.DataSiswa = s
			var lastTahfidz JurnalTahfidz
			DB.Where("siswa_id = ?", s.ID).Order("tanggal desc").First(&lastTahfidz)
			if lastTahfidz.ID != 0 {
				kpi.HafalanAkhir = fmt.Sprintf("Juz %s, %s", lastTahfidz.Juz, lastTahfidz.Surat)
			} else {
				kpi.HafalanAkhir = "Belum ada setoran"
			}
			kpi.TagihanAktif = true
			kpiList = append(kpiList, kpi)
		}
		return c.Render("dashboard", fiber.Map{"KPI": kpiList, "PPDB_Buka": setting.Value == "BUKA", "ActiveMenu": "home"})
	})

	wali.Get("/ppdb", func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		var setting Pengaturan
		DB.Where(&Pengaturan{Key: "STATUS_PPDB"}).First(&setting)
		if setting.Value == "TUTUP" {
			return c.Redirect("/wali/dashboard")
		}
		var s []Siswa
		DB.Where("akun_id = ?", sess.Get("akun_id")).Find(&s)
		return c.Render("wali_ppdb", fiber.Map{"DaftarSiswa": s, "PPDB_Buka": true, "ActiveMenu": "ppdb"})
	})

	wali.Post("/daftar", func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		sn := new(Siswa)
		c.BodyParser(sn)
		sn.AkunID = sess.Get("akun_id").(uint)
		flds := []string{"file_kk", "file_akta", "file_ktp_wali", "file_ijazah"}
		for _, f := range flds {
			fh, err := c.FormFile(f)
			if err == nil {
				loc := fmt.Sprintf("./uploads/%d_%s", time.Now().Unix(), fh.Filename)
				c.SaveFile(fh, loc)
				switch f {
				case "file_kk":
					sn.FileKK = loc
				case "file_akta":
					sn.FileAkta = loc
				case "file_ktp_wali":
					sn.FileKTPWali = loc
				case "file_ijazah":
					sn.FileIjazah = loc
				}
			}
		}
		DB.Create(&sn)
		return c.Redirect("/wali/ppdb")
	})

	wali.Get("/spp", func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		var setting Pengaturan
		DB.Where(&Pengaturan{Key: "STATUS_PPDB"}).First(&setting)
		var s []Siswa
		DB.Where("akun_id = ? AND status = ?", sess.Get("akun_id"), "Lulus").Find(&s)
		return c.Render("wali_spp", fiber.Map{"AnakLulus": s, "PPDB_Buka": setting.Value == "BUKA", "ActiveMenu": "spp"})
	})

	wali.Get("/inbox", func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		var setting Pengaturan
		DB.Where(&Pengaturan{Key: "STATUS_PPDB"}).First(&setting)
		var notifs []Notifikasi
		DB.Where("akun_id = ?", sess.Get("akun_id")).Order("tanggal desc").Find(&notifs)
		return c.Render("wali_inbox", fiber.Map{"Notifikasi": notifs, "PPDB_Buka": setting.Value == "BUKA", "ActiveMenu": "inbox"})
	})

	// === RUANG KENDALI ADMIN ===
	adm := app.Group("/admin", NoCache, MustLogin, MustAdmin)

	adm.Get("/dashboard", func(c *fiber.Ctx) error {
		var s []Siswa
		DB.Order("id desc").Find(&s)
		var setting Pengaturan
		DB.Where(&Pengaturan{Key: "STATUS_PPDB"}).First(&setting)
		return c.Render("admin", fiber.Map{"SemuaSiswa": s, "PPDB_Buka": setting.Value == "BUKA", "ActiveMenu": "ppdb"})
	})

	adm.Post("/toggle-ppdb", func(c *fiber.Ctx) error {
		// Pake format &Pengaturan{Key: ...} biar MySQL nggak salah paham
		DB.Model(&Pengaturan{}).Where(&Pengaturan{Key: "STATUS_PPDB"}).Update("value", c.FormValue("status_ppdb"))
		return c.Redirect("/admin/dashboard")
	})

	adm.Post("/update-status", func(c *fiber.Ctx) error {
		sID := c.FormValue("siswa_id")
		st := c.FormValue("status")
		DB.Model(&Siswa{}).Where("id = ?", sID).Update("status", st)
		var sn Siswa
		DB.Where("id = ?", sID).First(&sn)
		var ak Akun
		DB.Where("id = ?", sn.AkunID).First(&ak)
		if st == "Lulus" {
			go kirimNotif(ak.ID, ak.NoWA, "PPDB", "Pengumuman PPDB", fmt.Sprintf("🎉 *PPDB AR-ROMLAH*\nSelamat! Ananda *%s* dinyatakan LULUS.", sn.Nama))
		}
		return c.Redirect("/admin/dashboard")
	})

	adm.Get("/kesantrian/asrama", func(c *fiber.Ctx) error {
		var s []Siswa
		DB.Where("status = ?", "Lulus").Find(&s)
		return c.Render("asrama", fiber.Map{"SantriAktif": s, "ActiveMenu": "asrama"})
	})

	adm.Post("/kesantrian/asrama/set", func(c *fiber.Ctx) error {
		DB.Model(&Siswa{}).Where("id = ?", c.FormValue("siswa_id")).Update("kamar", c.FormValue("kamar"))
		return c.Redirect("/admin/kesantrian/asrama")
	})

	adm.Get("/akademik/tahfidz", func(c *fiber.Ctx) error {
		var s []Siswa
		DB.Where("status = ?", "Lulus").Find(&s)
		return c.Render("tahfidz", fiber.Map{"SantriAktif": s, "ActiveMenu": "tahfidz"})
	})

	adm.Post("/akademik/tahfidz/setor", func(c *fiber.Ctx) error {
		j := new(JurnalTahfidz)
		c.BodyParser(j)
		j.Tanggal = time.Now()
		DB.Create(&j)
		var sn Siswa
		DB.Where("id = ?", j.SiswaID).First(&sn)
		var ak Akun
		DB.Where("id = ?", sn.AkunID).First(&ak)
		go kirimNotif(ak.ID, ak.NoWA, "TAHFIDZ", "Laporan Hafalan", fmt.Sprintf("📖 Ananda *%s* hari ini setor hafalan Juz %s, Surat %s.", sn.Nama, j.Juz, j.Surat))
		return c.Redirect("/admin/akademik/tahfidz")
	})

	adm.Get("/keuangan/spp", func(c *fiber.Ctx) error {
		var s []Siswa
		DB.Where("status = ?", "Lulus").Find(&s)
		return c.Render("spp", fiber.Map{"SantriAktif": s, "ActiveMenu": "spp"})
	})

	adm.Post("/keuangan/spp/tagih", func(c *fiber.Ctx) error {
		var sn Siswa
		DB.Where("id = ?", c.FormValue("siswa_id")).First(&sn)
		var ak Akun
		DB.Where("id = ?", sn.AkunID).First(&ak)
		go kirimNotif(ak.ID, ak.NoWA, "SPP", "Tagihan SPP", fmt.Sprintf("🧾 Tagihan SPP ananda *%s* bulan ini sudah terbit.", sn.Nama))
		return c.Redirect("/admin/keuangan/spp")
	})

	log.Fatal(app.Listen(":3000"))
}
