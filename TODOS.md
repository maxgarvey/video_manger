
Please iteratively implement the following features, using best judgement.
Go one at a time, planning the change to behavior, then implementing the 
feature change, then implementing tests, then running tests, and then when
satisfied with the results, committing and pushing the changes. If expectations
aren't met, go back to planning with the new context in mind.

When you're planning, make files in a top level directory of the repository
called "plans" with a markdown file with the plan for the change in it.

I want you to proceed without intervention until you run out of credits and
then resume when you're able to again. Please update another document also
in the plans folder called "goals implementation status" with current status.

Before fine-grained planning, read the list and arrange them in the order that
makes the most sense. With a bias toward ease of complexity and implementation.

1. Make A button that calls to look up title, tag, data. Allow user to specify tv show name, episode name, movie title, etc. on an intermediary pop up form (modal perhaps?)
2. Create a migrations directory for further changes to schema and a way to build and tear down
3. Automatically tag video files with directory
4. Auto play a random episode on start
5. Add a configuration menu via another drawer/popup
6. Track watched video timestamps and don't re-show videos recently watched
7. Support like and double like
8. Support recursive directory scan for import
9. Online sharing on local network
10. Support P2P sharing
11. Can we leverage DNS at all for easier access?
12. Use yt-dlp to pull new videos and import them directly
13. Enable the creation of new folders
14. Add a functionality to put .mp4s on a USB stick in a format that BluRay players can read
15. Allow converting between video formats in the app

