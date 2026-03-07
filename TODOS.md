
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

Items 1-10 were implemented by a different AI and I don't have a lot of faith in their work. Can you please take stock of what they did to better understand it, and fix it where it's wrong. i think it was committing so git history may be instructive.

1. can you improve the trim video functionality to accept a draggable UI bar underneath the progress bar of the playthrough, representing the region to keep, and allow that to be finetuned vith finer-grained numerical inputs. On update, move the video to the exact spot the crop would take place
2. if you hit the crop button while the video is playing, stop playback and highlight the region from the current spot to the end of the video, effectively enabling easy deletion of the first part of a video
3. add clear labeling during trim mode that you're selecting the region to keep
4. make a confirmation after the user has highlighted a region or entered video times
5. organize season and episodes under specific shows
6. Add UI indicators on videos for if it's a tv show or a movie or a concert video or a vlog or a blog or a youtube
7. Support thumbnails in the UI for videos
8. Automatically generate a thumbnail image from the video. Create a button to randomly regenerate this
9. if the video has played to 70% or more, assume that we want to delete the end off the video by selecting the entire front region to keep when trim is clicked
10. Add a "Quck label" button to the info pane that will pop up a small modal to enter Title, Movie/TV, Season, Episode, Genre, etc.

These are new fetures and will require new planning and full process as described above.

11. improve playback. when i seek in videos it can kill the entire session so that I can't stream video any more.
12. Add ability to tweak the colors of the video playback like an old TV Tint, Hue, Saturation, Value, etc.
13. Make a button to delete the video being watched, it should delete the file and close the tab and update the library. And there should be a confirmation
14. Look for memory leaks
