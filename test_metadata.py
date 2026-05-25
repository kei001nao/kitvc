import asyncio
import musicbrainzngs
import os
import sys

# Add current directory to path so we can import our modules
sys.path.append(os.getcwd())

from kitvc.metadata_music import search_release, fetch_music_metadata

async def test_musicbrainz_lookup(artist, album):
    print(f"--- Testing MusicBrainz Lookup ---")
    print(f"Target: Artist='{artist}', Album='{album}'")
    
    # 1. Search for Release ID
    mbid = search_release(artist, album)
    if not mbid:
        print("Result: MBID not found via search.")
        return
    
    print(f"Result: Found MBID: {mbid}")
    
    # 2. Fetch Detailed Metadata
    metadata = fetch_music_metadata(mbid)
    if metadata:
        print("\n--- Metadata Found ---")
        print(f"Title:      {metadata.get('title')}")
        print(f"Release Date: {metadata.get('date')}")
        print(f"Country:    {metadata.get('country')}")
        print(f"Label:      {metadata.get('label')}")
        print(f"Barcode:    {metadata.get('barcode')}")
        print(f"Cover URL:  {metadata.get('cover_url')}")
        print(f"Comment:    {metadata.get('comment')}")
    else:
        print("Result: Failed to fetch detailed metadata.")

if __name__ == "__main__":
    # Test with a well-known album
    # You can change these values to test with your own media
    test_artist = "The Beatles"
    test_album = "Abbey Road"
    
    # Check if command line args provided
    if len(sys.argv) > 2:
        test_artist = sys.argv[1]
        test_album = sys.argv[2]
        
    asyncio.run(test_musicbrainz_lookup(test_artist, test_album))
