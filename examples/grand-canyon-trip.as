( ask "List 10 must-see viewpoints and attractions at Grand Canyon National Park with exact addresses"
  <*> places_search "campgrounds Grand Canyon Arizona"
  <*> places_search "hotels near Grand Canyon South Rim"
  <*> places_search "restaurants Grand Canyon Village"
)
>=> merge
>=> ask "Create a 3-day Grand Canyon itinerary combining the best viewpoints, camping/hotel options, and dining. Format with Day 1, Day 2, Day 3 sections."
>=> maps_trip "Grand Canyon Road Trip"
>=> form_create "Grand Canyon Trip RSVP"
>=> email "vinod.halaharvi@gmail.com"
