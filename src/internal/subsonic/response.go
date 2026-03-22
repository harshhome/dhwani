package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
)

const APIVersion = "1.16.1"

type Response struct {
	XMLName        xml.Name        `xml:"subsonic-response" json:"-"`
	Xmlns          string          `xml:"xmlns,attr,omitempty" json:"-"`
	Status         string          `xml:"status,attr" json:"status"`
	Version        string          `xml:"version,attr" json:"version"`
	Type           string          `xml:"type,attr" json:"type"`
	ServerVersion  string          `xml:"serverVersion,attr" json:"serverVersion"`
	OpenSubsonic   bool            `xml:"openSubsonic,attr" json:"openSubsonic"`
	Error          *Error          `xml:"error,omitempty" json:"error,omitempty"`
	License        *License        `xml:"license,omitempty" json:"license,omitempty"`
	MusicFolders   *MusicFolders   `xml:"musicFolders,omitempty" json:"musicFolders,omitempty"`
	Indexes        *Indexes        `xml:"indexes,omitempty" json:"indexes,omitempty"`
	Artists        *Artists        `xml:"artists,omitempty" json:"artists,omitempty"`
	MusicDirectory *MusicDirectory `xml:"musicDirectory,omitempty" json:"musicDirectory,omitempty"`
	AlbumList      *AlbumList      `xml:"albumList,omitempty" json:"albumList,omitempty"`
	AlbumList2     *AlbumList2     `xml:"albumList2,omitempty" json:"albumList2,omitempty"`
	Genres         *Genres         `xml:"genres,omitempty" json:"genres,omitempty"`
	ScanStatus     *ScanStatus     `xml:"scanStatus,omitempty" json:"scanStatus,omitempty"`
	User           *User           `xml:"user,omitempty" json:"user,omitempty"`
	ArtistInfo2    *ArtistInfo2    `xml:"artistInfo2,omitempty" json:"artistInfo2,omitempty"`
	ArtistInfo     *ArtistInfo     `xml:"artistInfo,omitempty" json:"artistInfo,omitempty"`
	AlbumInfo      *AlbumInfo      `xml:"albumInfo,omitempty" json:"albumInfo,omitempty"`
	AlbumInfo2     *AlbumInfo      `xml:"albumInfo2,omitempty" json:"albumInfo2,omitempty"`
	Playlists      *Playlists      `xml:"playlists,omitempty" json:"playlists,omitempty"`
	Playlist       *Playlist       `xml:"playlist,omitempty" json:"playlist,omitempty"`
	Starred        *Starred        `xml:"starred,omitempty" json:"starred,omitempty"`
	Starred2       *Starred2       `xml:"starred2,omitempty" json:"starred2,omitempty"`
	NowPlaying     *NowPlaying     `xml:"nowPlaying,omitempty" json:"nowPlaying,omitempty"`
	RandomSongs    *RandomSongs    `xml:"randomSongs,omitempty" json:"randomSongs,omitempty"`
	SongsByGenre   *SongsByGenre   `xml:"songsByGenre,omitempty" json:"songsByGenre,omitempty"`
	SearchResult2  *SearchResult   `xml:"searchResult2,omitempty" json:"searchResult2,omitempty"`
	SearchResult   *SearchResult   `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
	Lyrics         *Lyrics         `xml:"lyrics,omitempty" json:"lyrics,omitempty"`
	LyricsList     *LyricsList     `xml:"lyricsList,omitempty" json:"lyricsList,omitempty"`
	Song           *Song           `xml:"song,omitempty" json:"song,omitempty"`
	Artist         *Artist         `xml:"artist,omitempty" json:"artist,omitempty"`
	Album          *Album          `xml:"album,omitempty" json:"album,omitempty"`
}

type Error struct {
	Code    int    `xml:"code,attr" json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

type PayloadUnion struct {
	License        *License        `xml:"license,omitempty" json:"license,omitempty"`
	MusicFolders   *MusicFolders   `xml:"musicFolders,omitempty" json:"musicFolders,omitempty"`
	Indexes        *Indexes        `xml:"indexes,omitempty" json:"indexes,omitempty"`
	Artists        *Artists        `xml:"artists,omitempty" json:"artists,omitempty"`
	MusicDirectory *MusicDirectory `xml:"musicDirectory,omitempty" json:"musicDirectory,omitempty"`
	AlbumList      *AlbumList      `xml:"albumList,omitempty" json:"albumList,omitempty"`
	AlbumList2     *AlbumList2     `xml:"albumList2,omitempty" json:"albumList2,omitempty"`
	Genres         *Genres         `xml:"genres,omitempty" json:"genres,omitempty"`
	ScanStatus     *ScanStatus     `xml:"scanStatus,omitempty" json:"scanStatus,omitempty"`
	User           *User           `xml:"user,omitempty" json:"user,omitempty"`
	ArtistInfo2    *ArtistInfo2    `xml:"artistInfo2,omitempty" json:"artistInfo2,omitempty"`
	ArtistInfo     *ArtistInfo     `xml:"artistInfo,omitempty" json:"artistInfo,omitempty"`
	AlbumInfo      *AlbumInfo      `xml:"albumInfo,omitempty" json:"albumInfo,omitempty"`
	AlbumInfo2     *AlbumInfo      `xml:"albumInfo2,omitempty" json:"albumInfo2,omitempty"`
	Playlists      *Playlists      `xml:"playlists,omitempty" json:"playlists,omitempty"`
	Playlist       *Playlist       `xml:"playlist,omitempty" json:"playlist,omitempty"`
	Starred        *Starred        `xml:"starred,omitempty" json:"starred,omitempty"`
	Starred2       *Starred2       `xml:"starred2,omitempty" json:"starred2,omitempty"`
	NowPlaying     *NowPlaying     `xml:"nowPlaying,omitempty" json:"nowPlaying,omitempty"`
	RandomSongs    *RandomSongs    `xml:"randomSongs,omitempty" json:"randomSongs,omitempty"`
	SongsByGenre   *SongsByGenre   `xml:"songsByGenre,omitempty" json:"songsByGenre,omitempty"`
	SearchResult2  *SearchResult   `xml:"searchResult2,omitempty" json:"searchResult2,omitempty"`
	SearchResult   *SearchResult   `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
	Lyrics         *Lyrics         `xml:"lyrics,omitempty" json:"lyrics,omitempty"`
	LyricsList     *LyricsList     `xml:"lyricsList,omitempty" json:"lyricsList,omitempty"`
	Song           *Song           `xml:"song,omitempty" json:"song,omitempty"`
	Artist         *Artist         `xml:"artist,omitempty" json:"artist,omitempty"`
	Album          *Album          `xml:"album,omitempty" json:"album,omitempty"`
}

type License struct {
	Valid bool `xml:"valid,attr" json:"valid"`
}

type MusicFolders struct {
	Folders []MusicFolder `xml:"musicFolder" json:"musicFolder"`
}

type MusicFolder struct {
	ID   int    `xml:"id,attr" json:"id"`
	Name string `xml:"name,attr" json:"name"`
}

type Indexes struct {
	IgnoredArticles string   `xml:"ignoredArticles,attr,omitempty" json:"ignoredArticles,omitempty"`
	LastModified    int64    `xml:"lastModified,attr,omitempty" json:"lastModified,omitempty"`
	Index           []Index  `xml:"index" json:"index"`
	Shortcuts       []Artist `xml:"shortcut,omitempty" json:"shortcut,omitempty"`
}

type Index struct {
	Name    string   `xml:"name,attr" json:"name"`
	Artists []Artist `xml:"artist" json:"artist"`
}

type Artists struct {
	IgnoredArticles string  `xml:"ignoredArticles,attr,omitempty" json:"ignoredArticles,omitempty"`
	Index           []Index `xml:"index" json:"index"`
}

type AlbumList struct {
	Albums []Album `xml:"album,omitempty" json:"album,omitempty"`
}

type AlbumList2 struct {
	Albums []Album `xml:"album,omitempty" json:"album,omitempty"`
}

type Genres struct {
	Genre []Genre `xml:"genre" json:"genre"`
}

type ScanStatus struct {
	Scanning bool `xml:"scanning,attr" json:"scanning"`
	Count    int  `xml:"count,attr,omitempty" json:"count,omitempty"`
}

type User struct {
	Username            string `xml:"username,attr" json:"username"`
	Email               string `xml:"email,attr,omitempty" json:"email,omitempty"`
	ScrobblingEnabled   bool   `xml:"scrobblingEnabled,attr,omitempty" json:"scrobblingEnabled,omitempty"`
	AdminRole           bool   `xml:"adminRole,attr,omitempty" json:"adminRole,omitempty"`
	SettingsRole        bool   `xml:"settingsRole,attr,omitempty" json:"settingsRole,omitempty"`
	DownloadRole        bool   `xml:"downloadRole,attr,omitempty" json:"downloadRole,omitempty"`
	UploadRole          bool   `xml:"uploadRole,attr,omitempty" json:"uploadRole,omitempty"`
	PlaylistRole        bool   `xml:"playlistRole,attr,omitempty" json:"playlistRole,omitempty"`
	CoverArtRole        bool   `xml:"coverArtRole,attr,omitempty" json:"coverArtRole,omitempty"`
	CommentRole         bool   `xml:"commentRole,attr,omitempty" json:"commentRole,omitempty"`
	PodcastRole         bool   `xml:"podcastRole,attr,omitempty" json:"podcastRole,omitempty"`
	StreamRole          bool   `xml:"streamRole,attr,omitempty" json:"streamRole,omitempty"`
	JukeboxRole         bool   `xml:"jukeboxRole,attr,omitempty" json:"jukeboxRole,omitempty"`
	ShareRole           bool   `xml:"shareRole,attr,omitempty" json:"shareRole,omitempty"`
	VideoConversionRole bool   `xml:"videoConversionRole,attr,omitempty" json:"videoConversionRole,omitempty"`
}

type Genre struct {
	SongCount  int    `xml:"songCount,attr,omitempty" json:"songCount,omitempty"`
	AlbumCount int    `xml:"albumCount,attr,omitempty" json:"albumCount,omitempty"`
	Value      string `xml:",chardata" json:"value"`
}

type SearchResult struct {
	Artists []Artist `xml:"artist,omitempty" json:"artist,omitempty"`
	Albums  []Album  `xml:"album,omitempty" json:"album,omitempty"`
	Songs   []Song   `xml:"song,omitempty" json:"song,omitempty"`
}

type Lyrics struct {
	Artist string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	Title  string `xml:"title,attr,omitempty" json:"title,omitempty"`
	Value  string `xml:",chardata" json:"value"`
}

type LyricsList struct {
	StructuredLyrics []StructuredLyrics `xml:"structuredLyrics,omitempty" json:"structuredLyrics,omitempty"`
}

type StructuredLyrics struct {
	DisplayArtist string       `xml:"displayArtist,attr,omitempty" json:"displayArtist,omitempty"`
	DisplayTitle  string       `xml:"displayTitle,attr,omitempty" json:"displayTitle,omitempty"`
	Lang          string       `xml:"lang,attr,omitempty" json:"lang,omitempty"`
	Offset        int          `xml:"offset,attr,omitempty" json:"offset,omitempty"`
	Synced        bool         `xml:"synced,attr" json:"synced"`
	Line          []LyricsLine `xml:"line,omitempty" json:"line,omitempty"`
}

type LyricsLine struct {
	Start int64  `xml:"start,attr,omitempty" json:"start,omitempty"`
	Value string `xml:",chardata" json:"value"`
}

type MusicDirectory struct {
	Directory Directory `xml:"directory" json:"directory"`
}

type Directory struct {
	ID       string           `xml:"id,attr" json:"id"`
	Parent   string           `xml:"parent,attr,omitempty" json:"parent,omitempty"`
	Name     string           `xml:"name,attr" json:"name"`
	Starred  string           `xml:"starred,attr,omitempty" json:"starred,omitempty"`
	Children []DirectoryChild `xml:"child,omitempty" json:"child,omitempty"`
}

type DirectoryChild struct {
	ID          string `xml:"id,attr" json:"id"`
	Parent      string `xml:"parent,attr,omitempty" json:"parent,omitempty"`
	IsDir       bool   `xml:"isDir,attr" json:"isDir"`
	Title       string `xml:"title,attr" json:"title"`
	Album       string `xml:"album,attr,omitempty" json:"album,omitempty"`
	Artist      string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	Track       int    `xml:"track,attr,omitempty" json:"track,omitempty"`
	Year        int    `xml:"year,attr,omitempty" json:"year,omitempty"`
	CoverArt    string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Duration    int    `xml:"duration,attr,omitempty" json:"duration,omitempty"`
	BitRate     int    `xml:"bitRate,attr,omitempty" json:"bitRate,omitempty"`
	ContentType string `xml:"contentType,attr,omitempty" json:"contentType,omitempty"`
	ArtistID    string `xml:"artistId,attr,omitempty" json:"artistId,omitempty"`
	AlbumID     string `xml:"albumId,attr,omitempty" json:"albumId,omitempty"`
}

type ArtistInfo2 struct {
	Biography      string   `xml:"biography,omitempty" json:"biography,omitempty"`
	MusicBrainzID  string   `xml:"musicBrainzId,omitempty" json:"musicBrainzId,omitempty"`
	LastFMURL      string   `xml:"lastFmUrl,omitempty" json:"lastFmUrl,omitempty"`
	SmallImageURL  string   `xml:"smallImageUrl,omitempty" json:"smallImageUrl,omitempty"`
	MediumImageURL string   `xml:"mediumImageUrl,omitempty" json:"mediumImageUrl,omitempty"`
	LargeImageURL  string   `xml:"largeImageUrl,omitempty" json:"largeImageUrl,omitempty"`
	SimilarArtist  []Artist `xml:"similarArtist,omitempty" json:"similarArtist,omitempty"`
}

type ArtistInfo struct {
	Biography      string   `xml:"biography,omitempty" json:"biography,omitempty"`
	MusicBrainzID  string   `xml:"musicBrainzId,omitempty" json:"musicBrainzId,omitempty"`
	LastFMURL      string   `xml:"lastFmUrl,omitempty" json:"lastFmUrl,omitempty"`
	SmallImageURL  string   `xml:"smallImageUrl,omitempty" json:"smallImageUrl,omitempty"`
	MediumImageURL string   `xml:"mediumImageUrl,omitempty" json:"mediumImageUrl,omitempty"`
	LargeImageURL  string   `xml:"largeImageUrl,omitempty" json:"largeImageUrl,omitempty"`
	SimilarArtist  []Artist `xml:"similarArtist,omitempty" json:"similarArtist,omitempty"`
}

type AlbumInfo struct {
	Notes string `xml:"notes,omitempty" json:"notes,omitempty"`
}

type Playlists struct {
	Items []Playlist `xml:"playlist" json:"playlist"`
}

type Playlist struct {
	ID        string `xml:"id,attr,omitempty" json:"id,omitempty"`
	Name      string `xml:"name,attr,omitempty" json:"name,omitempty"`
	Owner     string `xml:"owner,attr,omitempty" json:"owner,omitempty"`
	Public    bool   `xml:"public,attr,omitempty" json:"public,omitempty"`
	SongCount int    `xml:"songCount,attr,omitempty" json:"songCount,omitempty"`
	Duration  int    `xml:"duration,attr,omitempty" json:"duration,omitempty"`
	Created   string `xml:"created,attr,omitempty" json:"created,omitempty"`
	Changed   string `xml:"changed,attr,omitempty" json:"changed,omitempty"`
	CoverArt  string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Entries   []Song `xml:"entry,omitempty" json:"entry,omitempty"`
}

type Starred struct {
	Artists []Artist `xml:"artist" json:"artist"`
	Albums  []Album  `xml:"album" json:"album"`
	Songs   []Song   `xml:"song" json:"song"`
}

type Starred2 struct {
	Artists []Artist `xml:"artist" json:"artist"`
	Albums  []Album  `xml:"album" json:"album"`
	Songs   []Song   `xml:"song" json:"song"`
}

type NowPlaying struct {
	Entries []Song `xml:"entry" json:"entry"`
}

type RandomSongs struct {
	Songs []Song `xml:"song" json:"song"`
}

type SongsByGenre struct {
	Songs []Song `xml:"song" json:"song"`
}

type Artist struct {
	ID             string  `xml:"id,attr" json:"id"`
	Name           string  `xml:"name,attr" json:"name"`
	AlbumCount     int     `xml:"albumCount,attr,omitempty" json:"albumCount,omitempty"`
	CoverArt       string  `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	ArtistImageURL string  `xml:"artistImageUrl,attr,omitempty" json:"artistImageUrl,omitempty"`
	Albums         []Album `xml:"album,omitempty" json:"album,omitempty"`
}

type Album struct {
	ID            string `xml:"id,attr" json:"id"`
	Name          string `xml:"name,attr" json:"name"`
	Artist        string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	DisplayArtist string `xml:"displayArtist,attr,omitempty" json:"displayArtist,omitempty"`
	ArtistID      string `xml:"artistId,attr,omitempty" json:"artistId,omitempty"`
	SongCount     int    `xml:"songCount,attr,omitempty" json:"songCount,omitempty"`
	Duration      int    `xml:"duration,attr,omitempty" json:"duration,omitempty"`
	Created       string `xml:"created,attr,omitempty" json:"created,omitempty"`
	Year          int    `xml:"year,attr,omitempty" json:"year,omitempty"`
	CoverArt      string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Songs         []Song `xml:"song,omitempty" json:"song,omitempty"`
}

type Song struct {
	ID                 string       `xml:"id,attr" json:"id"`
	Parent             string       `xml:"parent,attr,omitempty" json:"parent,omitempty"`
	Title              string       `xml:"title,attr" json:"title"`
	Album              string       `xml:"album,attr,omitempty" json:"album,omitempty"`
	Artist             string       `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	DisplayArtist      string       `xml:"displayArtist,attr,omitempty" json:"displayArtist,omitempty"`
	Artists            []SongArtist `xml:"artists,omitempty" json:"artists,omitempty"`
	AlbumArtists       []SongArtist `xml:"albumArtists,omitempty" json:"albumArtists,omitempty"`
	DisplayAlbumArtist string       `xml:"displayAlbumArtist,attr,omitempty" json:"displayAlbumArtist,omitempty"`
	Track              int          `xml:"track,attr,omitempty" json:"track,omitempty"`
	Year               int          `xml:"year,attr,omitempty" json:"year,omitempty"`
	CoverArt           string       `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Size               int64        `xml:"size,attr,omitempty" json:"size,omitempty"`
	ContentType        string       `xml:"contentType,attr,omitempty" json:"contentType,omitempty"`
	Suffix             string       `xml:"suffix,attr,omitempty" json:"suffix,omitempty"`
	Duration           int          `xml:"duration,attr,omitempty" json:"duration,omitempty"`
	BitRate            int          `xml:"bitRate,attr,omitempty" json:"bitRate,omitempty"`
	Path               string       `xml:"path,attr,omitempty" json:"path,omitempty"`
	IsVideo            bool         `xml:"isVideo,attr,omitempty" json:"isVideo,omitempty"`
	Type               string       `xml:"type,attr,omitempty" json:"type,omitempty"`
	DiscNumber         int          `xml:"discNumber,attr,omitempty" json:"discNumber,omitempty"`
	Created            string       `xml:"created,attr,omitempty" json:"created,omitempty"`
	ArtistID           string       `xml:"artistId,attr,omitempty" json:"artistId,omitempty"`
	AlbumID            string       `xml:"albumId,attr,omitempty" json:"albumId,omitempty"`
}

type SongArtist struct {
	ID   string `xml:"id,attr,omitempty" json:"id,omitempty"`
	Name string `xml:"name,attr,omitempty" json:"name,omitempty"`
}

func NewSuccess(payload *PayloadUnion) Response {
	resp := Response{
		Status:        "ok",
		Version:       APIVersion,
		Type:          "dhwani",
		ServerVersion: "0.0.3",
		OpenSubsonic:  true,
		Xmlns:         "http://subsonic.org/restapi",
	}
	if payload != nil {
		resp.License = payload.License
		resp.MusicFolders = payload.MusicFolders
		resp.Indexes = payload.Indexes
		resp.Artists = payload.Artists
		resp.MusicDirectory = payload.MusicDirectory
		resp.AlbumList = payload.AlbumList
		resp.AlbumList2 = payload.AlbumList2
		resp.Genres = payload.Genres
		resp.ScanStatus = payload.ScanStatus
		resp.User = payload.User
		resp.ArtistInfo2 = payload.ArtistInfo2
		resp.ArtistInfo = payload.ArtistInfo
		resp.AlbumInfo = payload.AlbumInfo
		resp.AlbumInfo2 = payload.AlbumInfo2
		resp.Playlists = payload.Playlists
		resp.Playlist = payload.Playlist
		resp.Starred = payload.Starred
		resp.Starred2 = payload.Starred2
		resp.NowPlaying = payload.NowPlaying
		resp.RandomSongs = payload.RandomSongs
		resp.SongsByGenre = payload.SongsByGenre
		resp.SearchResult2 = payload.SearchResult2
		resp.SearchResult = payload.SearchResult
		resp.Lyrics = payload.Lyrics
		resp.LyricsList = payload.LyricsList
		resp.Song = payload.Song
		resp.Artist = payload.Artist
		resp.Album = payload.Album
	}
	return resp
}

func NewError(code int, msg string) Response {
	return Response{
		Status:        "failed",
		Version:       APIVersion,
		Type:          "dhwani",
		ServerVersion: "0.0.3",
		OpenSubsonic:  true,
		Xmlns:         "http://subsonic.org/restapi",
		Error:         &Error{Code: code, Message: msg},
	}
}

func Write(w http.ResponseWriter, r *http.Request, status int, resp Response) {
	format := r.URL.Query().Get("f")
	if format == "json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]Response{"subsonic-response": resp})
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	_ = enc.Encode(resp)
}
